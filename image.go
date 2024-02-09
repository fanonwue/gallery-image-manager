package main

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Image struct {
	gorm.Model
	Name             string `gorm:"size:50"`
	Title            string `gorm:"size:50"`
	Description      string
	Nsfw             bool
	Format           string `gorm:"size:5"`
	NoResize         bool
	IgnoreAuthorName bool
	ImageExists      bool
	AuthorID         uint
	Author           *Author
	Categories       []*Category `gorm:"many2many:images_categories"`
	Related          []*Image    `gorm:"many2many:images_relations;association_jointable_foreignkey:related_id"`
}

type ImageDto struct {
	ID               uint       `binding:"-" json:"id" yaml:"id"`
	Name             string     `json:"name" yaml:"name"`
	Title            string     `json:"title" yaml:"title"`
	Description      string     `json:"description" yaml:"description"`
	Nsfw             *bool      `json:"nsfw" yaml:"nsfw"`
	Format           string     `json:"format" yaml:"format"`
	NoResize         *bool      `json:"noResize" yaml:"noResize"`
	IgnoreAuthorName *bool      `json:"ignoreAuthorName" yaml:"ignoreAuthorName"`
	AuthorID         uint       `json:"authorId" yaml:"authorId"`
	Author           *AuthorDto `json:"author" yaml:"author"`
	Categories       []uint     `json:"categories" yaml:"categories"`
}

type ImageView struct {
	ID            uint
	Name          string
	Title         string
	AuthorName    string
	AuthorID      uint
	Nsfw          bool
	ImageExists   bool
	Format        string
	Description   string
	Categories    []uint
	CategoryNames []string
	RelatedIds    []uint
	Related       map[uint]string
}

func (i *Image) toDto() ImageDto {
	dto := ImageDto{
		ID:               i.ID,
		Name:             i.Name,
		Title:            i.Title,
		Format:           i.Format,
		NoResize:         &i.NoResize,
		Description:      i.Description,
		IgnoreAuthorName: &i.IgnoreAuthorName,
		Nsfw:             &i.Nsfw,
	}

	if i.Categories != nil {
		dto.Categories = Map(i.Categories, func(cat *Category) uint {
			return cat.ID
		})
	}

	if i.Author != nil {
		authorDto := i.Author.toDto()
		dto.Author = &authorDto
	}

	return dto
}

func (i *Image) toView() ImageView {
	view := ImageView{
		ID:            i.ID,
		Name:          i.Name,
		Title:         i.Title,
		Description:   i.Description,
		Nsfw:          i.Nsfw,
		Format:        i.Format,
		ImageExists:   i.ImageExists,
		Categories:    make([]uint, len(i.Categories)),
		CategoryNames: make([]string, len(i.Categories)),
		Related:       make(map[uint]string, len(i.Related)),
		RelatedIds:    make([]uint, len(i.Related)),
	}

	if i.Author != nil {
		view.AuthorName = i.Author.Name
		view.AuthorID = i.AuthorID
	}

	if i.Categories != nil {
		for idx, cat := range i.Categories {
			view.Categories[idx] = cat.ID
			view.CategoryNames[idx] = cat.DisplayName
		}
	}

	if i.Related != nil {
		for idx, related := range i.Related {
			stringName := fmt.Sprintf("%s - %s", related.Name, related.Title)
			view.Related[related.ID] = stringName
			view.RelatedIds[idx] = related.ID
		}
	}

	return view
}

func (i *Image) updateWithDto(dto ImageDto) {
	if len(dto.Name) > 0 {
		i.Name = dto.Name
	}
	if len(dto.Description) > 0 {
		i.Description = dto.Description
	}
	if len(dto.Title) > 0 {
		i.Title = dto.Title
	}
	if len(dto.Format) > 0 {
		i.Format = dto.Format
	}

	if dto.Author != nil && dto.Author.ID > 0 {
		author := Author{}
		author.ID = dto.Author.ID
		i.Author = &author
	} else if dto.AuthorID > 0 {
		i.AuthorID = dto.AuthorID
		newAuthor := Author{}
		newAuthor.ID = dto.AuthorID
		i.Author = &newAuthor
	}

	if dto.Categories != nil {
		if len(dto.Categories) > 0 {
			i.Categories = Map(dto.Categories, func(id uint) *Category {
				cat := Category{}
				cat.ID = id
				return &cat
			})
		} else {
			i.Categories = make([]*Category, 0)
		}
	}
	if dto.NoResize != nil {
		i.NoResize = *dto.NoResize
	}
	if dto.IgnoreAuthorName != nil {
		i.IgnoreAuthorName = *dto.IgnoreAuthorName
	}
	if dto.Nsfw != nil {
		i.Nsfw = *dto.Nsfw
	}
}

func (i *ImageDto) toModel() Image {
	image := Image{
		Nsfw:             false,
		NoResize:         false,
		IgnoreAuthorName: false,
	}
	image.updateWithDto(*i)
	return image
}

func (i *Image) OriginalFilePath() string {
	return fmt.Sprintf("%s.%s", appConfig.OriginalDir+strconv.Itoa(int(i.ID)), i.Format)
}

func (i *Image) ImageIdentifier() string {
	if i.IgnoreAuthorName {
		return strings.ToLower(i.Name)
	}

	authorName := "UNKNOWN_AUTHOR"
	if i.Author != nil {
		authorName = i.Author.Name
	}
	return strings.ToLower(fmt.Sprintf("%s-%s", authorName, i.Name))
}

func (i *Image) relatedImageIds() []uint {
	ids := make([]uint, 0)
	for _, related := range i.Related {
		if related != nil {
			ids = append(ids, related.ID)
		}
	}
	return ids
}

func (i *Image) categoryIds() []uint {
	ids := make([]uint, 0)
	for _, category := range i.Categories {
		if category != nil {
			ids = append(ids, category.ID)
		}
	}
	return ids
}

const (
	imageIdName = "imageId"
)

// ------------- WEBSERVER HANDLER -------------

func fetchImages(c *gin.Context) ([]*Image, *ListFilter, error) {
	var images []*Image
	var filter ListFilter

	tx := db.Preload("Author").Preload("Categories")

	rawCategory := c.Query("category")
	if len(rawCategory) == 0 {
		rawCategory = c.Param(categoryIdName)
	}

	if len(rawCategory) > 0 {
		categoryId, err := strconv.ParseUint(rawCategory, 0, 64)
		if err != nil {
			c.Error(err)
			c.String(400, err.Error())
			return nil, nil, err
		}
		filter.Category = uint(categoryId)
		if categoryId > 0 {
			tx = tx.Joins("INNER JOIN images_categories ic ON ic.image_id = images.id AND ic.category_id = ?", categoryId)
		}
	}

	rawAuthor := c.Query("author")
	if len(rawAuthor) == 0 {
		rawAuthor = c.Param(authorIdName)
	}

	if len(rawAuthor) > 0 {
		authorId, err := strconv.ParseUint(rawAuthor, 0, 64)
		if err != nil {
			c.Error(err)
			c.String(400, err.Error())
			return nil, nil, err
		}
		filter.Author = uint(authorId)
		if authorId > 0 {
			tx = tx.Where(&Image{AuthorID: filter.Author})
		}
	}

	rawNsfw := c.Query("nsfw")
	if len(rawNsfw) > 0 {
		nsfw, err := strconv.ParseBool(rawNsfw)
		if err != nil {
			c.Error(err)
			c.String(400, err.Error())
			return nil, nil, err
		}
		filter.Nsfw = &nsfw
		tx = tx.Where("nsfw = ?", nsfw)
	}

	sortMode := strings.ToLower(c.Query("sortMode"))
	if sortMode != "desc" {
		sortMode = "asc"
	}

	filter.SortMode = sortMode

	sortBy := c.Query("sortBy")
	if len(sortBy) == 0 {
		sortBy = "id"
	}
	filter.SortBy = sortBy
	switch sortBy {
	case "id":
		tx = tx.Order("id " + sortMode)
	case "name":
		tx = tx.Order("name " + sortMode)
	case "title":
		tx = tx.Order("title " + sortMode)
	}

	res := tx.Find(&images)

	if res.Error != nil {
		c.Error(res.Error)
		c.String(500, res.Error.Error())
		return nil, nil, res.Error
	}

	return images, &filter, nil

}

func getImagesHtml(c *gin.Context) {
	images, filter, err := fetchImages(c)
	if err != nil {
		return
	}

	viewImages := Map(images, func(image *Image) ImageView {
		return image.toView()
	})

	c.HTML(200, "images.gohtml", gin.H{
		"images":     viewImages,
		"filter":     filter,
		"authors":    getAllAuthors(),
		"categories": getAllCategories(),
	})
}

func loadImage(c *gin.Context) (*Image, error) {
	id, err := pathIdToInt(imageIdName, c)
	if err != nil {
		idValue := c.Param(imageIdName)
		if idValue == entityNew {
			return &Image{}, nil
		}
		c.String(400, err.Error())
		return nil, err
	}

	var image Image
	result := db.Preload(clause.Associations).First(&image, id)

	if result.RowsAffected == 0 {
		c.Error(result.Error)
		c.String(404, "Image with id '%d' not found", id)
		return nil, result.Error
	}

	return &image, nil
}

func getImageHtml(c *gin.Context) {
	image, err := loadImage(c)

	if err == nil {
		c.HTML(200, "image.gohtml", gin.H{
			"image":      image.toView(),
			"authors":    getAllAuthors(),
			"categories": getAllCategories(),
		})
	}
}

func updateImageForm(c *gin.Context) {
	image, err := loadImage(c)
	if err != nil {
		return
	}

	switch c.PostForm("action") {
	case "save":
		isNewImage := image.ID == 0

		dto := ImageDto{
			Name:        c.PostForm("name"),
			Title:       c.PostForm("title"),
			Description: c.PostForm("description"),
		}

		rawNsfw := c.PostForm("nsfw")
		if len(rawNsfw) > 0 {
			nsfw, err := strconv.ParseBool(rawNsfw)
			if err == nil {
				dto.Nsfw = &nsfw
			}
		}

		rawAuthor := c.PostForm("author")
		if len(rawAuthor) > 0 {
			authorId, err := strconv.ParseUint(rawAuthor, 0, 64)
			if err == nil {
				dto.AuthorID = uint(authorId)
			}
		}

		newCategories := make([]*Category, 0)
		rawNewCategories := c.PostFormArray("categories")
		for _, rawCategoryId := range rawNewCategories {
			categoryId, err := strconv.ParseUint(rawCategoryId, 0, 64)
			if err != nil {
				continue
			}
			newCat := Category{}
			newCat.ID = uint(categoryId)
			newCategories = append(newCategories, &newCat)
		}

		relatedParts := strings.Split(c.PostForm("related"), ",")
		newRelatedImages := make([]*Image, 0)
		for _, rawRelated := range relatedParts {
			relatedId, err := strconv.ParseUint(strings.TrimSpace(rawRelated), 0, 64)
			if err != nil {
				continue
			}
			newRelated := Image{}
			newRelated.ID = uint(relatedId)
			newRelatedImages = append(newRelatedImages, &newRelated)
		}

		image.updateWithDto(dto)

		tx := db.Session(&gorm.Session{})
		res := tx.Save(&image)
		if res.Error != nil {
			c.Error(res.Error)
			c.String(500, "Error updating image: %v", res.Error)
			return
		}
		if newCategories != nil {
			err = tx.Model(&image).Association("Categories").Replace(&newCategories)
			if err != nil {
				c.Error(err)
				c.String(500, "Error updating category associations: %v", err)
				return
			}
		}
		if newRelatedImages != nil {
			// Delete old relations to this image
			if image.Related != nil && len(image.Related) > 0 {
				err = tx.Model(image.Related).Association("Related").Delete(&image)
				if err != nil {
					c.Error(err)
					c.String(500, "Error deleting old related images associations: %v", err)
					return
				}
			}

			err = tx.Model(&image).Association("Related").Replace(&newRelatedImages)
			if err != nil {
				c.Error(err)
				c.String(500, "Error updating related images associations: %v", err)
				return
			}

			relationsToCurrentImage := Map(newRelatedImages, func(related *Image) any {
				return image
			})

			err = tx.Model(&newRelatedImages).Association("Related").Append(relationsToCurrentImage...)
			if err != nil {
				c.Error(err)
				c.String(500, "Error update related images relation: %v", err)
				return
			}
		}
		tx.Commit()

		if isNewImage {
			c.Redirect(302, fmt.Sprintf("/images/%d", image.ID))
		} else {
			getImageHtml(c)
		}
	case "delete":
		db.Delete(&image)
		c.Redirect(302, "/images")
	}
}

func uploadImageForm(c *gin.Context) {
	image, err := loadImage(c)
	if err != nil {
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.Error(err)
		c.String(400, "Error uploading file: %v", err)
		return
	}

	oldFileName := strconv.Itoa(int(image.ID)) + "." + image.Format

	// Check whether old file exists and delete it if it exits
	if _, err := os.Stat(oldFileName); err == nil {
		err = os.Remove(path.Join(appConfig.OriginalDir, oldFileName))
		if err != nil {
			c.Error(err)
			c.String(500, "Could not remove old file: %v", err)
			return
		}
	}

	extension := filepath.Ext(file.Filename)
	newFileName := strconv.Itoa(int(image.ID)) + extension

	err = c.SaveUploadedFile(file, path.Join(appConfig.OriginalDir, newFileName))
	if err != nil {
		c.Error(err)
		c.String(500, "Error saving file: %v", err)
		return
	}

	image.Format = extension[1:]
	image.ImageExists = true

	db.Save(&image)

	_, processAfterUpload := c.GetPostForm("process")
	if processAfterUpload {
		_, err = processImage(image)
		if err != nil {
			c.Error(err)
			c.String(500, "Error processing file after upload: %v", err)
			return
		}
	}

	c.Redirect(302, fmt.Sprintf("/images/%d", image.ID))

}

func getImages(c *gin.Context) {
	images, _, err := fetchImages(c)
	if err != nil {
		return
	}

	imagesDto := Map(images, func(image *Image) ImageDto {
		return image.toDto()
	})

	c.JSON(200, &imagesDto)
}

func getImage(c *gin.Context) {
	id, err := pathIdToInt(imageIdName, c)
	if err != nil {
		c.String(400, err.Error())
	}

	var image Image
	result := db.Preload(clause.Associations).First(&image, id)

	if result.RowsAffected == 0 {
		c.String(404, "Image with id '%d' not found", id)
		return
	}

	c.JSON(http.StatusOK, image.toDto())
}

func addImage(c *gin.Context) {
	imageDto := ImageDto{}
	if err := c.ShouldBind(&imageDto); err != nil {
		c.String(http.StatusBadRequest, "Could not bind body to DTO: %v", err)
		return
	}

	image := imageDto.toModel()

	result := db.Create(&image)

	if result.Error != nil {
		c.String(http.StatusInternalServerError, "Error inserting category: %v", result.Error)
		return
	}

	c.JSON(http.StatusOK, image.toDto())
}

func updateImage(c *gin.Context) {
	id, err := pathIdToInt(imageIdName, c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
	}

	imageDto := ImageDto{}
	if err := c.ShouldBind(&imageDto); err != nil {
		c.String(http.StatusBadRequest, "Could not bind body to DTO: %v", err)
		return
	}

	image := Image{}
	image.ID = id

	res := db.First(&image)
	if res.RowsAffected == 0 {
		c.String(http.StatusNotFound, "Image with ID '%s' not found", id)
		return
	}

	image.updateWithDto(imageDto)
	res = db.Save(&image)
	if res.Error != nil {
		c.String(http.StatusInternalServerError, "Error updating image with ID '%s': %v", id, res.Error)
		return
	}

	c.JSON(http.StatusOK, image.toDto())
}

func deleteImage(c *gin.Context) {
	id, err := pathIdToInt(imageIdName, c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
	}

	image := Image{}
	image.ID = id

	res := db.Delete(&image)
	if res.Error != nil {
		c.String(http.StatusInternalServerError, "Error deleting image with ID '%s': %v", id, res.Error)
		return
	}

	c.Status(200)
}

func processImages(c *gin.Context) {
	var images []Image

	db.Preload(clause.Associations).Find(&images)

	err := os.RemoveAll(appConfig.ProcessedDir)
	if err != nil {
		c.String(500, c.Error(err).Error())
		return
	}

	err = createDirIfNotExists(appConfig.ProcessedDir)
	if err != nil {
		c.String(500, c.Error(err).Error())
		return
	}

	wg := sync.WaitGroup{}
	resultChannel := make(chan *ImageProcessResult, len(images))
	results := make([]*ImageProcessResult, 0, len(images))

	for i := range images {
		wg.Add(1)
		// Go will override variables in the for loop before the goroutine starts
		// Grab the value directly from the array instead
		image := images[i]
		go processImageAsync(&image, resultChannel, &wg)
	}

	go func() {
		wg.Wait()
		close(resultChannel)
	}()

	for result := range resultChannel {
		results = append(results, result)
	}

	jsonBytes, err := json.Marshal(&results)
	if err != nil {
		c.String(500, c.Error(err).Error())
	}
	err = os.WriteFile(path.Join(appConfig.ProcessedDir, "images.json"), jsonBytes, 0666)
	if err != nil {
		c.String(500, c.Error(err).Error())
	}

	c.JSON(200, &results)
}
