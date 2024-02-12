package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

type (
	MetaImageCollection []MetaImage

	MetaImage struct {
		ID               int             `yaml:"id"`
		Name             string          `yaml:"name"`
		Title            string          `yaml:"title"`
		Description      string          `yaml:"description"`
		Nsfw             bool            `yaml:"nsfw"`
		Format           string          `yaml:"format"`
		AuthorName       string          `yaml:"author"`
		CategoryNames    []string        `yaml:"categories"`
		Related          []int           `yaml:"related"`
		IgnoreAuthorName bool            `yaml:"ignoreAuthorName"`
		NoResize         bool            `yaml:"noResize"`
		Author           *MetaAuthor     `yaml:"-"`
		Categories       []*MetaCategory `yaml:"-"`
	}

	MetaAuthor struct {
		Name string `yaml:"name"`
		Url  string `yaml:"url"`
	}

	MetaCategory struct {
		Name        string `yaml:"name"`
		DisplayName string `yaml:"displayName"`
		Description string `yaml:"description"`
		Nsfw        bool   `yaml:"nsfw"`
		Show        *bool  `yaml:"show"`
	}
)

const (
	galleryLibraryDefaultFormat = "webp"
	importIdOffset              = 19
)

var (
	descriptionRegexReplace = regexp.MustCompile(`\\\s`)
	nameIdMap               = map[string]uint{
		"favicon":                  1,
		"profile-banner":           2,
		"profile-banner-christmas": 3,
	}
)

func importMeta(libraryPath string) MetaImageCollection {
	var err error
	metaPath := path.Join(libraryPath, "meta")

	authorDefData, err := os.ReadFile(path.Join(metaPath, "authors.yml"))
	if err != nil {
		logger.Errorf("Could not open authors.yml: %v", err)
	}

	var authorDefinitions []*MetaAuthor
	err = yaml.Unmarshal(authorDefData, &authorDefinitions)

	authorMap := map[string]*MetaAuthor{}

	for _, author := range authorDefinitions {
		authorMap[strings.ToLower(author.Name)] = author
	}

	categoryDefData, err := os.ReadFile(path.Join(metaPath, "categories.yml"))
	if err != nil {
		logger.Errorf("Could not open categories.yml: %v", err)
	}

	var categoryDefinitions []*MetaCategory
	err = yaml.Unmarshal(categoryDefData, &categoryDefinitions)

	categoryMap := map[string]*MetaCategory{}

	for _, cat := range categoryDefinitions {
		if cat.Show == nil {
			defaultShow := true
			cat.Show = &defaultShow

		}

		categoryMap[strings.ToLower(cat.Name)] = cat
	}

	imageDefData, err := os.ReadFile(path.Join(metaPath, "images.yml"))
	if err != nil {
		logger.Errorf("Could not open images.yml: %v", err)
	}

	var imageDefinitions MetaImageCollection
	err = yaml.Unmarshal(imageDefData, &imageDefinitions)

	for i := range imageDefinitions {
		image := &imageDefinitions[i]

		image.Author = authorMap[strings.ToLower(image.AuthorName)]
		image.Categories = Map(image.CategoryNames, func(name string) *MetaCategory {
			return categoryMap[strings.ToLower(name)]
		})

		if len(image.Format) == 0 {
			image.Format = galleryLibraryDefaultFormat
		}
	}

	return imageDefinitions
}

func importGalleryLibrary(libraryPath string) error {
	metaImages := importMeta(libraryPath)

	err := truncateTables()
	if err != nil {
		return err
	}

	tx := db.Session(&gorm.Session{SkipHooks: true})

	faviconImage := Image{
		Name:        "favicon",
		Title:       "FAVICON",
		Description: "Favicon",
	}
	tx.Create(&faviconImage)

	var images []*Image

	for idx, meta := range metaImages {
		author := Author{}

		res := tx.Where("name = ?", meta.Author.Name).Limit(1).Find(&author)

		if res.RowsAffected == 0 {
			// Author not already in DB
			author.Name = meta.Author.Name
			author.Url = meta.Author.Url
			tx.Create(&author)
		}

		var categories []*Category

		for _, metaCategory := range meta.Categories {
			category := Category{}

			res = tx.Where("name = ?", metaCategory.Name).Limit(1).Find(&category)

			if res.RowsAffected == 0 {
				category.Name = metaCategory.Name
				category.DisplayName = metaCategory.DisplayName
				category.Description = metaCategory.Description
				category.Show = *metaCategory.Show
				category.Nsfw = metaCategory.Nsfw

				tx.Create(&category)
			}

			categories = append(categories, &category)
		}

		image := Image{
			Name:             meta.Name,
			Title:            meta.Title,
			Description:      formatDescription(meta.Description),
			Nsfw:             meta.Nsfw,
			Format:           meta.Format,
			NoResize:         meta.NoResize,
			IgnoreAuthorName: meta.IgnoreAuthorName,
			Author:           &author,
			Categories:       categories,
			SortIndex:        (idx + 1) * 10,
		}

		image.ID = uint(meta.ID + importIdOffset)

		mappedId, found := nameIdMap[meta.Name]
		if found {
			image.ID = mappedId
		}

		res = tx.Create(&image)

		if res.Error != nil {
			return res.Error
		}

		images = append(images, &image)
	}

	for _, meta := range metaImages {
		image := Image{}
		image.ID = uint(meta.ID + importIdOffset)
		mappedId, found := nameIdMap[meta.Name]
		if found {
			image.ID = mappedId
		}
		tx.First(&image)

		relatedImages := Map(meta.Related, func(r int) *Image {
			relatedImage := Image{}
			relatedImage.ID = uint(r + importIdOffset)
			return &relatedImage
		})

		err := tx.Model(&image).Association("Related").Append(&relatedImages)
		if err != nil {
			logger.Errorf("Error while adding related images: %v", err)
		}
	}

	tx.Commit()

	// Clear original folder
	err = os.RemoveAll(path.Join(appConfig.OriginalDir))
	if err != nil {
		return err
	}

	err = createDirIfNotExists(appConfig.OriginalDir)
	if err != nil {
		return err
	}

	for _, image := range images {
		copyImageFile(image.ID, libraryPath)
	}

	return nil

}

func copyImageFile(imageId uint, libraryPath string) {
	image := Image{}
	image.ID = imageId
	res := db.Preload("Author").First(&image)
	if res.RowsAffected == 0 {
		logger.Errorf("Could not find image for ID '%d' while trying to copy file", imageId)
		return
	}

	format := strings.ToLower(image.Format)

	sourceFileName := fmt.Sprintf("%s.%s", image.ImageIdentifier(), format)
	sourcePath := path.Join(libraryPath, sourceFileName)
	source, err := os.Open(sourcePath)
	if err != nil {
		logger.Errorf("Could not open source file: %v", err)
		return
	}
	defer source.Close()

	destinationFileName := fmt.Sprintf("%d.%s", image.ID, format)
	destination, err := os.Create(path.Join(appConfig.OriginalDir, destinationFileName))
	if err != nil {
		logger.Errorf("Could not create destination file: %v", err)
		return
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	if err != nil {
		logger.Errorf("Could not copy file: %v", err)
	}

	image.ImageExists = true
	db.Save(&image)
}

func formatDescription(description string) string {
	return descriptionRegexReplace.ReplaceAllString(description, "\n")
}

func truncateTables() error {
	tables := []string{"images_categories", "images", "authors", "icons", "images_relations", "image_variants"}

	for _, table := range tables {
		res := db.Exec("DELETE FROM " + table)
		if res.Error != nil {
			return res.Error
		}

		res = db.Exec("UPDATE sqlite_sequence SET seq = 0 WHERE name = ?", table)
		if res.Error != nil {
			return res.Error
		}
	}

	res := db.Unscoped().Delete(&Category{}, "name NOT IN ?", reservedCategories)
	if res.Error != nil {
		return res.Error
	}
	db.Exec("UPDATE sqlite_sequence SET seq = ? WHERE name = ?", len(reservedCategories), "categories")

	return nil
}
