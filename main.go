package main

import (
	"encoding/json"
	"errors"
	"fmt"
	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"html/template"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type (
	AppConfig struct {
		DataDir      string
		ProcessedDir string
		FaviconDir   string
		OriginalDir  string
		ImportDir    string
		DbLocation   string
		PasswordHash string
	}

	Account struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	ListFilter struct {
		Author   uint
		Nsfw     *bool
		Category uint
		SortBy   string
		SortMode string
	}
)

const (
	apiPrefix = "/v1"
	entityNew = "new"

	SORT_ASC  = iota
	SORT_DESC = iota
)

var (
	appConfig     *AppConfig
	accounts      gin.Accounts
	sessionTokens = map[string]string{}
	db            *gorm.DB
	logger        *zap.SugaredLogger
	sortModeMap   = map[string]int{
		"asc":  SORT_ASC,
		"desc": SORT_DESC,
	}
)

func Map[T, U any](ts []T, f func(T) U) []U {
	us := make([]U, len(ts))
	for i := range ts {
		us[i] = f(ts[i])
	}
	return us
}

func readAccounts() {
	accountData, err := os.ReadFile("data/accounts.json")
	if err != nil {
		logger.Panicf("Error reading accounts file: %v", err)
	}

	var readAccounts []Account

	err = json.Unmarshal(accountData, &readAccounts)
	if err != nil {
		logger.Panicf("Error unmarshaling accounts file: %v", err)
	}

	newAccounts := gin.Accounts{}
	for _, account := range readAccounts {
		newAccounts[account.Username] = account.Password
	}

	accounts = newAccounts
}

func hashPassword(pw []byte) string {
	hash, err := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost)
	if err != nil {
		logger.Errorf("Error hashing password: %v", err)
	}
	return string(hash)
}

func checkPassword(plainPw []byte) bool {
	hashedPw := []byte(appConfig.PasswordHash)

	err := bcrypt.CompareHashAndPassword(hashedPw, plainPw)
	return err != nil
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

func createConfig() *AppConfig {
	config := AppConfig{
		DataDir: "data/",
		//ProcessedDir: "/mnt/m/Web/senex-gallery-content/managed/",
		DbLocation: "/mnt/d/Sqlite/image-manager.db",
		ImportDir:  "/mnt/m/Web/senex-gallery-content",
	}

	config.ProcessedDir = path.Join(config.DataDir, "images/processed")
	config.OriginalDir = path.Join(config.DataDir, "images/originals")
	config.FaviconDir = path.Join(config.DataDir, "icons")

	appConfig = &config
	return appConfig
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

func login(c *gin.Context) {

}

func apiPath(pathTemplate string, idNames ...any) string {
	return apiPrefix + fmt.Sprintf(pathTemplate, idNames...)
}

func createDirIfNotExists(dir string) error {
	return os.MkdirAll(dir, os.ModePerm)
}

func setup() {
	setupLogging()
	createConfig()
	createDirIfNotExists(appConfig.DataDir)
	createDirIfNotExists(appConfig.OriginalDir)
	createDirIfNotExists(appConfig.ProcessedDir)
	createDirIfNotExists(appConfig.FaviconDir)
}

func main() {
	setup()
	var err error

	tmpDb, err := gorm.Open(sqlite.Open(appConfig.DbLocation))

	if err != nil {
		logger.Panicf("Could not open sqlite database: %v", err)
	}

	db = tmpDb

	err = db.AutoMigrate(&Image{}, &Category{}, &Author{}, &ImageVariant{}, &Icon{})
	if err != nil {
		logger.Panicf("Error migrating models: %v", err)
	}

	createReservedCategories()

	readAccounts()

	r := gin.New()
	r.Use(ginzap.Ginzap(logger.Desugar(), time.RFC3339, false))
	r.Use(ginzap.RecoveryWithZap(logger.Desugar(), true))
	r.SetTrustedProxies(nil)
	r.SetFuncMap(template.FuncMap{
		"joinStrings": strings.Join,
		"joinUints": func(elems []uint, sep string) string {
			return strings.Trim(strings.Join(strings.Fields(fmt.Sprint(elems)), sep), "[]")
		},
		"isNull": func(b *bool) bool { return b == nil },
		"notNullAndFalse": func(b *bool) bool {
			return b != nil && *b == false
		},
		"notNullAndTrue": func(b *bool) bool {
			return b != nil && *b
		},
		"isNullOrTrue": func(b *bool) bool {
			return b == nil || *b
		},
		"categorySelected": func(category CategoryDto, selectedCategories []uint) bool {
			for _, categoryId := range selectedCategories {
				if category.ID == categoryId {
					return true
				}
			}
			return false
		},
		"derefBool": func(value *bool) bool {
			return *value
		},
	})
	r.LoadHTMLGlob("resources/ui/*")

	authorized := r.Group("", gin.BasicAuth(accounts))

	authorized.GET("/", func(c *gin.Context) {
		c.HTML(200, "landing.gohtml", gin.H{})
	})

	authorized.GET("/images", getImagesHtml)
	authorized.POST("/images/process", processImages)
	authorized.POST("/images/process-icons", processFaviconApi)
	authorized.GET(fmt.Sprintf("/images/:%s", imageIdName), getImageHtml)
	authorized.POST(fmt.Sprintf("/images/:%s", imageIdName), updateImageForm)
	authorized.POST(fmt.Sprintf("/images/:%s/upload", imageIdName), uploadImageForm)

	authorized.GET("/authors", getAuthorsHtml)
	authorized.GET(fmt.Sprintf("/authors/:%s", authorIdName), getAuthorHtml)
	authorized.POST(fmt.Sprintf("/authors/:%s", authorIdName), updateAuthorForm)

	authorized.GET("/categories", getCategoriesHtml)
	authorized.GET(fmt.Sprintf("/categories/:%s", categoryIdName), getCategoryHtml)
	authorized.POST(fmt.Sprintf("/categories/:%s", categoryIdName), updateCategoryForm)

	r.Static("/files/originals", appConfig.OriginalDir)
	r.Static("/files/processed", appConfig.ProcessedDir)
	r.Static("/files/icons", appConfig.FaviconDir)

	r.POST(apiPath("/auth/login"))

	r.GET(apiPath("/categories"), getCategories)
	authorized.PUT(apiPath("/categories"), addCategory)
	r.GET(apiPath("/categories/:%s", categoryIdName), getCategory)
	r.GET(apiPath("/categories/:%s/images", categoryIdName), getImages)
	authorized.PATCH(apiPath("/categories/:%s", categoryIdName), updateCategory)
	authorized.DELETE(apiPath("/categories/:%s", categoryIdName), deleteCategory)

	r.GET(apiPath("/images"), getImages)
	authorized.PUT(apiPath("/images"), addImage)
	r.GET(apiPath("/images/:%s", imageIdName), getImage)
	r.GET(apiPath("/icons"), getIcons)
	authorized.PATCH(apiPath("/images/:%s", imageIdName), updateImage)
	authorized.DELETE(apiPath("/images/:%s", imageIdName), deleteImage)

	authorized.POST(apiPath("/images/process"), processImages)

	r.GET(apiPath("/authors"), getAuthors)
	authorized.PUT(apiPath("/authors"), addAuthor)
	r.GET(apiPath("/authors/:%s", authorIdName), getAuthor)
	authorized.PATCH(apiPath("/authors/:%s", authorIdName), updateAuthor)
	authorized.DELETE(apiPath("/authors/:%s", authorIdName), deleteAuthor)

	err = r.Run(":3000")
	if err != nil {
		logger.Errorf("Error starting web server: %v", err)
	}
}
