package main

import (
	"fmt"
	"gallery-image-manager/util"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"slices"
	"strings"
)

type Author struct {
	gorm.Model
	Name   string `gorm:"uniqueIndex;size:50"`
	Url    string
	Images []Image
}

type AuthorDto struct {
	ID         uint   `binding:"-" json:"id" yaml:"id"`
	Name       string `json:"name" yaml:"name"`
	Url        string `json:"url" yaml:"url"`
	ImageCount uint   `json:"-" yaml:"-"`
}

func (a *Author) toDto() AuthorDto {
	return AuthorDto{
		ID:   a.ID,
		Name: a.Name,
		Url:  a.Url,
	}
}

func (a *Author) toDtoWithImageCount() AuthorDto {
	imageCount := db.Model(&a).Association("Images").Count()
	dto := a.toDto()
	dto.ImageCount = uint(imageCount)
	return dto
}

func (a *Author) updateWithDto(dto AuthorDto) {
	if len(dto.Name) > 0 {
		a.Name = dto.Name
	}
	if len(dto.Url) > 0 {
		a.Url = dto.Url
	}
}

func (a *AuthorDto) toModel() Author {
	return Author{
		Name: a.Name,
		Url:  a.Url,
	}
}

const (
	authorIdName = "authorId"
)

// ------------- WEBSERVER HANDLER -------------

func getAuthorsHtml(c *gin.Context) {
	var authors []Author
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

	tx.Find(&authors)

	authorsDto := Map(authors, func(a Author) AuthorDto {
		return a.toDtoWithImageCount()
	})

	if filter.SortBy == "imageCount" {
		slices.SortFunc(authorsDto, func(a, b AuthorDto) int {
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

	c.HTML(200, "authors.gohtml", gin.H{
		"authors": authorsDto,
		"filter":  filter,
	})
}

func loadAuthor(c *gin.Context) (*Author, error) {
	id, err := pathIdToInt(authorIdName, c)
	if err != nil {
		idValue := c.Param(authorIdName)
		if idValue == entityNew {
			return &Author{}, nil
		}
		c.String(400, err.Error())
		return nil, err
	}

	var author Author
	result := db.Preload(clause.Associations).First(&author, id)

	if result.RowsAffected == 0 {
		c.Error(result.Error)
		c.String(404, "Author with id '%d' not found", id)
		return nil, result.Error
	}

	return &author, nil
}

func getAuthorHtml(c *gin.Context) {
	author, err := loadAuthor(c)

	if err == nil {
		c.HTML(200, "author.gohtml", gin.H{
			"author": author.toDtoWithImageCount(),
		})
	}
}

func updateAuthorForm(c *gin.Context) {
	author, err := loadAuthor(c)
	if err != nil {
		return
	}

	switch c.PostForm("action") {
	case "save":
		isNewAuthor := author.ID == 0

		dto := AuthorDto{
			Name: c.PostForm("name"),
			Url:  c.PostForm("url"),
		}

		author.updateWithDto(dto)

		db.Save(&author)

		if isNewAuthor {
			c.Redirect(302, fmt.Sprintf("/authors/%d", author.ID))
		} else {
			getAuthorHtml(c)
		}
	case "delete":
		db.Unscoped().Delete(&author)
		c.Redirect(302, "/authors")
	}
}

func getAllAuthors() []AuthorDto {
	var authors []Author
	db.Find(&authors)

	// Case Insensitive sorting in SQLite is vendor specific (using "COLLATE NOCSAE"), so to keep it independent,
	// just sort the list of authors after it's retrieved
	slices.SortFunc(authors, func(a, b Author) int {
		return util.CompareCaseInsensitive(a.Name, b.Name)
	})

	authorsDto := Map(authors, func(a Author) AuthorDto {
		return a.toDto()
	})
	return authorsDto
}

func getAuthors(c *gin.Context) {
	var authors []Author
	db.Find(&authors)

	authorsDto := Map(authors, func(author Author) AuthorDto {
		return author.toDto()
	})

	c.JSON(200, &authorsDto)
}

func getAuthor(c *gin.Context) {
	id, err := pathIdToInt(authorIdName, c)
	if err != nil {
		c.String(400, err.Error())
	}

	var author Author
	result := db.First(&author, id)

	if result.RowsAffected == 0 {
		c.String(404, "Author with id '%d' not found", id)
		return
	}

	c.JSON(http.StatusOK, author.toDto())
}

func addAuthor(c *gin.Context) {
	authorDto := AuthorDto{}
	if err := c.ShouldBind(&authorDto); err != nil {
		c.String(http.StatusBadRequest, "Could not bind body to DTO: %v", err)
		return
	}

	author := authorDto.toModel()

	result := db.Create(&author)

	if result.Error != nil {
		c.String(http.StatusInternalServerError, "Error inserting author: %v", result.Error)
		return
	}

	c.JSON(http.StatusOK, author.toDto())
}

func updateAuthor(c *gin.Context) {
	id, err := pathIdToInt(authorIdName, c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
	}

	authorDto := AuthorDto{}
	if err := c.ShouldBind(&authorDto); err != nil {
		c.String(http.StatusBadRequest, "Could not bind body to DTO: %v", err)
		return
	}

	author := Author{}
	author.ID = id

	res := db.First(&author)
	if res.RowsAffected == 0 {
		c.String(http.StatusNotFound, "Author with ID '%s' not found", id)
		return
	}

	author.updateWithDto(authorDto)
	res = db.Save(&author)
	if res.Error != nil {
		c.String(http.StatusInternalServerError, "Error updating author with ID '%s': %v", id, res.Error)
		return
	}

	c.JSON(http.StatusOK, author.toDto())
}

func deleteAuthor(c *gin.Context) {
	id, err := pathIdToInt(authorIdName, c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
	}

	author := Author{}
	author.ID = id

	res := db.Delete(&author)
	if res.Error != nil {
		c.String(http.StatusInternalServerError, "Error deleting author with ID '%s': %v", id, res.Error)
		return
	}

	c.Status(200)
}
