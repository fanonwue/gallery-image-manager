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
	AuthorID         uint
	Author           *Author
	Categories       []*Category `gorm:"many2many:images_categories"`
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

func (i *Image) toDto() ImageDto {
	dto := ImageDto{
		ID:               i.ID,
		Name:             i.Name,
		Title:            i.Title,
		Format:           i.Format,
		NoResize:         &i.NoResize,
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

func (i *Image) updateWithDto(dto ImageDto) {
	if len(dto.Name) > 0 {
		i.Name = dto.Name
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
	}

	if dto.Categories != nil && len(dto.Categories) > 0 {
		i.Categories = Map(dto.Categories, func(id uint) *Category {
			cat := Category{}
			cat.ID = id
			return &cat
		})
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
	return fmt.Sprintf("%s.%s", originalImageDir+strconv.Itoa(int(i.ID)), i.Format)
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

const (
	imageIdName = "imageId"
)

// ------------- WEBSERVER HANDLER -------------

func getImages(c *gin.Context) {
	var images []Image

	tx := db.Preload("Author").Preload("Categories")

	rawCategory := c.Query("category")
	if len(rawCategory) == 0 {
		rawCategory = c.Param(categoryIdName)
	}

	if len(rawCategory) > 0 {
		categoryId, err := strconv.Atoi(rawCategory)
		if err != nil {
			c.Error(err)
			c.String(400, err.Error())
			return
		}
		tx = tx.Joins("INNER JOIN images_categories ic ON ic.image_id = images.id AND ic.category_id = ?", categoryId)
	}

	res := tx.Find(&images)

	if res.Error != nil {
		c.Error(res.Error)
		c.String(500, res.Error.Error())
		return
	}

	imagesDto := Map(images, func(image Image) ImageDto {
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

	db.Preload("Author").Limit(10).Find(&images)

	err := os.RemoveAll(processedImageDir)
	if err != nil {
		c.String(500, c.Error(err).Error())
		return
	}

	err = createDirIfNotExists(processedImageDir)
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
	err = os.WriteFile(path.Join(processedImageDir, "images.json"), jsonBytes, 0666)
	if err != nil {
		c.String(500, c.Error(err).Error())
	}

	c.JSON(200, &results)
}
