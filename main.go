package main

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"os"
	"strconv"
)

const (
	processedImageDir = "data/images/processed/"
	originalImageDir  = "data/images/originals/"
	dbPath            = "/mnt/d/Sqlite/image-manager.db"
	apiPrefix         = "/v1"
)

var (
	imageDefaultNsfw = true

	db     *gorm.DB
	logger *zap.SugaredLogger
)

func Map[T, U any](ts []T, f func(T) U) []U {
	us := make([]U, len(ts))
	for i := range ts {
		us[i] = f(ts[i])
	}
	return us
}

func setupLogging() {
	logConfig := zap.NewDevelopmentConfig()
	baseLogger, _ := logConfig.Build()
	defer baseLogger.Sync()
	logger = baseLogger.Sugar()

	// Set zap's globals
	zap.ReplaceGlobals(baseLogger)

	// Set global logger as well
	_, err := zap.RedirectStdLogAt(baseLogger, logConfig.Level.Level())
	if err != nil {
		logger.Errorf("Could not set global logger: %v", err)
	}
}

func pathIdToInt(idName string, c *gin.Context) (uint, error) {
	rawId := c.Param(idName)

	if len(rawId) == 0 {
		return 0, errors.New("no id specified")
	}

	id, err := strconv.Atoi(rawId)

	if err != nil {
		return 0, err
	}

	return uint(id), nil
}

func apiPath(pathTemplate string, idNames ...any) string {
	return apiPrefix + fmt.Sprintf(pathTemplate, idNames...)
}

func createDirIfNotExists(dir string) error {
	return os.MkdirAll(dir, os.ModePerm)
}

func main() {
	setupLogging()
	var err error

	tmpDb, err := gorm.Open(sqlite.Open(dbPath))

	if err != nil {
		logger.Panicf("Could not open sqlite database: %v", err)
	}

	db = tmpDb

	err = db.AutoMigrate(&Image{}, &Category{}, &Author{})
	if err != nil {
		logger.Panicf("Error migrating models: %v", err)
	}

	//importGalleryLibrary("/mnt/d/senex-gallery-content/")

	r := gin.Default()
	r.SetTrustedProxies(nil)

	r.GET(apiPath("/categories"), getCategories)
	r.PUT(apiPath("/categories"), addCategory)
	r.GET(apiPath("/categories/:%s", categoryIdName), getCategory)
	r.GET(apiPath("/categories/:%s/images", categoryIdName), getImages)
	r.PATCH(apiPath("/categories/:%s", categoryIdName), updateCategory)
	r.DELETE(apiPath("/categories/:%s", categoryIdName), deleteCategory)

	r.GET(apiPath("/images"), getImages)
	r.PUT(apiPath("/images"), addImage)
	r.GET(apiPath("/images/:%s", imageIdName), getImage)
	r.PATCH(apiPath("/images/:%s", imageIdName), updateImage)
	r.DELETE(apiPath("/images/:%s", imageIdName), deleteImage)

	r.POST(apiPath("/images/process"), processImages)

	r.GET(apiPath("/authors"), getAuthors)
	r.PUT(apiPath("/authors"), addAuthor)
	r.GET(apiPath("/authors/:%s", authorIdName), getAuthor)
	r.PATCH(apiPath("/authors/:%s", authorIdName), updateAuthor)
	r.DELETE(apiPath("/authors/:%s", authorIdName), deleteAuthor)

	err = r.Run(":3000")
	if err != nil {
		logger.Errorf("Error starting web server: %v", err)
	}
}
