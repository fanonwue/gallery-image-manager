package main

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
)

type Category struct {
	gorm.Model
	Name        string `gorm:"uniqueIndex;size:50"`
	DisplayName string `gorm:"size:100"`
	Description string
	Show        bool
	Nsfw        bool
	Images      []*Image `gorm:"many2many:images_categories"`
}

type CategoryDto struct {
	ID          uint   `binding:"-" json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	Description string `json:"description" yaml:"description"`
	Show        *bool  `json:"show" yaml:"show"`
	Nsfw        *bool  `json:"nsfw" yaml:"nsfw"`
}

func (c *Category) toDto() CategoryDto {
	return CategoryDto{
		ID:          c.ID,
		Name:        c.Name,
		DisplayName: c.DisplayName,
		Description: c.Description,
		Show:        &c.Show,
		Nsfw:        &c.Nsfw,
	}
}

func (c *Category) updateWithDto(dto CategoryDto) {
	if len(dto.Name) > 0 {
		c.Name = dto.Name
	}
	if len(dto.DisplayName) > 0 {
		c.DisplayName = dto.DisplayName
	} else if len(c.DisplayName) == 0 {
		c.DisplayName = c.Name
	}
	if len(dto.Description) > 0 {
		c.Description = dto.Description
	}
	if dto.Show != nil {
		c.Show = *dto.Show
	}
	if dto.Nsfw != nil {
		c.Nsfw = *dto.Nsfw
	}
}

func (c *CategoryDto) toModel() Category {
	category := Category{
		Show: true,
		Nsfw: false,
	}
	category.updateWithDto(*c)
	return category
}

const (
	categoryIdName = "categoryId"
)

// ------------- WEBSERVER HANDLER -------------

func getCategories(c *gin.Context) {
	var categories []Category
	db.Find(&categories)

	categoriesDto := Map(categories, func(category Category) CategoryDto {
		return category.toDto()
	})

	c.JSON(200, &categoriesDto)
}

func getCategory(c *gin.Context) {
	id, err := pathIdToInt(categoryIdName, c)
	if err != nil {
		c.String(400, err.Error())
	}

	var category Category
	result := db.First(&category, id)

	if result.RowsAffected == 0 {
		c.String(404, "Category with id '%d' not found", id)
		return
	}

	c.JSON(http.StatusOK, category.toDto())
}

func addCategory(c *gin.Context) {
	categoryDto := CategoryDto{}
	if err := c.ShouldBind(&categoryDto); err != nil {
		c.String(http.StatusBadRequest, "Could not bind body to DTO: %v", err)
		return
	}

	category := categoryDto.toModel()

	result := db.Create(&category)

	if result.Error != nil {
		c.String(http.StatusInternalServerError, "Error inserting category: %v", result.Error)
		return
	}

	c.JSON(http.StatusOK, category.toDto())
}

func updateCategory(c *gin.Context) {
	id, err := pathIdToInt(categoryIdName, c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
	}

	categoryDto := CategoryDto{}
	if err := c.ShouldBind(&categoryDto); err != nil {
		c.String(http.StatusBadRequest, "Could not bind body to DTO: %v", err)
		return
	}

	category := Category{}
	category.ID = id

	res := db.First(&category)
	if res.RowsAffected == 0 {
		c.String(http.StatusNotFound, "Category with ID '%s' not found", id)
		return
	}

	category.updateWithDto(categoryDto)
	res = db.Save(&category)
	if res.Error != nil {
		c.String(http.StatusInternalServerError, "Error updating category with ID '%s': %v", id, res.Error)
		return
	}

	c.JSON(http.StatusOK, category.toDto())
}

func deleteCategory(c *gin.Context) {
	id, err := pathIdToInt(categoryIdName, c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
	}

	category := Category{}
	category.ID = id

	res := db.Delete(&category)
	if res.Error != nil {
		c.String(http.StatusInternalServerError, "Error updating category with ID '%s': %v", id, res.Error)
		return
	}

	c.Status(200)
}
