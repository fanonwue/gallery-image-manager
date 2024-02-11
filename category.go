package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"slices"
	"strings"
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
	ImageCount  uint   `json:"-" yaml:"-"`
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

func (c *Category) toDtoWithImageCount() CategoryDto {
	imageCount := db.Model(&c).Association("Images").Count()
	dto := c.toDto()
	dto.ImageCount = uint(imageCount)
	return dto
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
	categoryIdName      = "categoryId"
	faviconCategoryName = "favicon"
	profileCategoryName = "profile"
)

var (
	reservedCategories = []string{
		faviconCategoryName, profileCategoryName,
	}
)

func createReservedCategories() {

	db.Transaction(func(tx *gorm.DB) error {
		for _, categoryName := range reservedCategories {
			res := tx.Where("name = ?", categoryName).Find(&Category{})
			if res.RowsAffected > 0 {
				continue
			}

			cat := Category{
				Name:        categoryName,
				DisplayName: strings.ToTitle(categoryName),
				Description: "Reserved Category",
			}

			res = tx.Create(&cat)
			if res.Error != nil {
				return res.Error
			}
			logger.Infof("Created reserved category \"%s\"", categoryName)
		}
		return nil
	})
}

// ------------- WEBSERVER HANDLER -------------

func getCategoriesHtml(c *gin.Context) {
	var categories []Category
	filter := ListFilter{}

	tx := db.Session(&gorm.Session{})

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
	}

	tx.Find(&categories)

	categoriesDto := Map(categories, func(a Category) CategoryDto {
		return a.toDtoWithImageCount()
	})

	if filter.SortBy == "imageCount" {
		slices.SortFunc(categoriesDto, func(a, b CategoryDto) int {
			ret := int(a.ImageCount) - int(b.ImageCount)
			if ret == 0 {
				ret = int(a.ID) - int(b.ID)
			}

			if sortModeMap[sortMode] == SORT_DESC {
				ret *= -1
			}

			return ret
		})
	}

	c.HTML(200, "categories.gohtml", gin.H{
		"categories": categoriesDto,
		"filter":     filter,
	})
}

func loadCategory(c *gin.Context) (*Category, error) {
	id, err := pathIdToInt(categoryIdName, c)
	if err != nil {
		idValue := c.Param(categoryIdName)
		if idValue == entityNew {
			return &Category{
				Show: true,
			}, nil
		}
		c.String(400, err.Error())
		return nil, err
	}

	var category Category
	result := db.Preload(clause.Associations).First(&category, id)

	if result.RowsAffected == 0 {
		c.Error(result.Error)
		c.String(404, "Category with id '%d' not found", id)
		return nil, result.Error
	}

	return &category, nil
}

func getCategoryHtml(c *gin.Context) {
	category, err := loadCategory(c)

	if err == nil {
		c.HTML(200, "category.gohtml", gin.H{
			"category": category.toDtoWithImageCount(),
		})
	}
}

func updateCategoryForm(c *gin.Context) {
	category, err := loadCategory(c)
	if err != nil {
		return
	}

	switch c.PostForm("action") {
	case "save":
		isNewCategory := category.ID == 0

		_, show := c.GetPostForm("show")
		_, nsfw := c.GetPostFormArray("nsfw")

		dto := CategoryDto{
			Name:        c.PostForm("name"),
			DisplayName: c.PostForm("displayName"),
			Description: c.PostForm("description"),
			Nsfw:        &nsfw,
			Show:        &show,
		}

		category.updateWithDto(dto)

		db.Save(&category)

		if isNewCategory {
			c.Redirect(302, fmt.Sprintf("/categories/%d", category.ID))
		} else {
			getCategoryHtml(c)
		}
	case "delete":
		db.Unscoped().Delete(&category)
		c.Redirect(302, "/categories")
	}

}

func getAllCategories() []CategoryDto {
	var categories []Category
	db.Order("Show DESC").Order("Name ASC").Find(&categories)

	categoriesDto := Map(categories, func(a Category) CategoryDto {
		return a.toDto()
	})
	return categoriesDto
}

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
