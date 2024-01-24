package main

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
)

type Author struct {
	gorm.Model
	Name   string `gorm:"uniqueIndex;size:50"`
	Url    string
	Images []Image
}

type AuthorDto struct {
	ID   uint   `binding:"-" json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
	Url  string `json:"url" yaml:"url"`
}

func (a *Author) toDto() AuthorDto {
	return AuthorDto{
		ID:   a.ID,
		Name: a.Name,
		Url:  a.Url,
	}
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
