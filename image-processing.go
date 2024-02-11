package main

import (
	"fmt"
	"github.com/h2non/bimg"
	"math"
	"os"
	"path"
	"path/filepath"
	"sync"
)

type (
	ProcessingRule struct {
		Quality      int
		MaxDim       int
		Height       int
		Width        int
		Suffix       string
		NoSizeSuffix bool
		Name         string
		Enlarge      bool
		Background   *bimg.Color
		Format       bimg.ImageType
	}

	ImageOptions struct {
		Image         *Image
		Data          *[]byte
		ProcRule      ProcessingRule
		HeightLimited bool
		TargetPath    string
	}

	ImageProcessResult struct {
		ImageID     uint                    `json:"imageId" yaml:"imageId"`
		Name        string                  `json:"name" yaml:"name"`
		Title       string                  `json:"title" yaml:"title"`
		Description string                  `json:"description" yaml:"description"`
		Related     []uint                  `json:"related" yaml:"related"`
		Categories  []uint                  `json:"categories" yaml:"categories"`
		Author      uint                    `json:"author" yaml:"author"`
		Nsfw        bool                    `json:"nsfw" yaml:"nsfw"`
		Original    *ProcessedImageVariant  `json:"original" yaml:"original"`
		Variants    []ProcessedImageVariant `json:"variants" yaml:"variants"`
	}

	FaviconProcessResult struct {
		Name     string                  `json:"name" yaml:"name"`
		Author   uint                    `json:"author" yaml:"author"`
		Nsfw     bool                    `json:"nsfw" yaml:"nsfw"`
		Variants []ProcessedImageVariant `json:"variants" yaml:"variants"`
	}

	ProcessedImageVariant struct {
		Height   int    `json:"height" yaml:"height"`
		Width    int    `json:"width" yaml:"width"`
		Format   string `json:"format" yaml:"format"`
		FileName string `json:"fileName" yaml:"fileName"`
		Quality  int    `json:"quality" yaml:"quality"`
		Suffix   string `json:"suffix,omitempty" yaml:"suffix,omitempty"`
		Name     string `json:"name,omitempty" yaml:"name,omitempty"`
	}

	ImageProcessConfig struct {
		Image           *Image
		TargetPath      string
		ProcessRules    []ProcessingRule
		ProcessOriginal bool
	}
)

const (
	defaultImageFormat   = bimg.WEBP
	defaultImageQuality  = 75
	highImageQuality     = 90
	originalImageQuality = 95
	defaultFaviconSize   = 192
)

var (
	defaultProcRules []ProcessingRule
	originalProcRule *ProcessingRule
)

func faviconProcessRules() []ProcessingRule {
	pwaBackground := &bimg.Color{R: 67, G: 118, B: 198}

	formats := []bimg.ImageType{
		defaultImageFormat,
		bimg.PNG,
	}

	baseRule := ProcessingRule{
		Enlarge: true,
		Quality: originalImageQuality,
		Name:    "favicon",
	}

	rawRules := []ProcessingRule{
		{
			MaxDim: 32,
		},
		{
			MaxDim: 48,
		},
		{
			MaxDim: 96,
		},
		{
			MaxDim:     96,
			Background: pwaBackground,
			Name:       "pwa-icon",
		},
		{
			MaxDim: 180,
		},
		{
			MaxDim: 192,
		},
		{
			MaxDim: 512,
		},
		{
			MaxDim:     512,
			Background: pwaBackground,
			Name:       "pwa-icon",
		},
		{
			MaxDim: 167,
		},
		{
			MaxDim: 600,
			Name:   "senex-profile",
		},
	}

	rules := make([]ProcessingRule, 0, len(rawRules)*len(formats))

	for _, rule := range rawRules {
		rule.Quality = baseRule.Quality
		rule.Format = baseRule.Format
		rule.Enlarge = true

		if len(rule.Name) == 0 {
			rule.Name = baseRule.Name
		}

		for _, format := range formats {
			newRule := rule
			newRule.Format = format
			rules = append(rules, newRule)
		}
	}

	return rules
}

func defaultProcessingRules() []ProcessingRule {
	if defaultProcRules == nil {

		metaMaxHeight := 1000
		metaMaxWidth := int(math.Round(float64(metaMaxHeight) * 1.91))

		defaultProcRules = []ProcessingRule{
			{
				Quality: defaultImageQuality,
				MaxDim:  900,
				Format:  defaultImageFormat,
			},
			{
				Quality: defaultImageQuality,
				MaxDim:  1200,
				Format:  defaultImageFormat,
			},
			{
				Quality: defaultImageQuality,
				MaxDim:  2400,
				Format:  defaultImageFormat,
			},
			{
				Quality: highImageQuality,
				MaxDim:  3000,
				Format:  defaultImageFormat,
			},
			{
				Quality: highImageQuality,
				Width:   metaMaxWidth,
				Height:  metaMaxHeight,
				Suffix:  "meta",
				Format:  bimg.PNG,
			},
		}
	}

	return defaultProcRules
}

func originalProcessingRule() ProcessingRule {
	if originalProcRule == nil {
		originalProcRule = &ProcessingRule{
			NoSizeSuffix: true,
			Quality:      originalImageQuality,
			Format:       defaultImageFormat,
			MaxDim:       3000,
		}
	}

	return *originalProcRule
}

func processImageAsync(config *ImageProcessConfig, targetChannel chan<- *ImageProcessResult, wg *sync.WaitGroup) {
	defer wg.Done()
	result, err := processImage(config)
	if err == nil && result != nil {
		targetChannel <- result
	}
}

func processImage(config *ImageProcessConfig) (*ImageProcessResult, error) {
	image := config.Image

	// Delete old processed images
	oldFiles, err := filepath.Glob(fmt.Sprintf("%s/%s*", appConfig.ProcessedDir, image.ImageIdentifier()))
	for _, file := range oldFiles {
		_ = os.Remove(file)
	}

	imageFile, err := os.ReadFile(image.OriginalFilePath())
	if err != nil {
		logger.Errorf("Could not read image file: %v", err)
		return nil, err
	}

	size, err := imageSizeFromBytes(&imageFile)
	if err != nil {
		return nil, err
	}

	heightLimited := size.Height > size.Width

	wg := sync.WaitGroup{}

	procRules := config.ProcessRules

	if len(procRules) == 0 {
		procRules = defaultProcessingRules()
	}

	channel := make(chan ProcessedImageVariant, len(procRules))

	for i := range procRules {
		wg.Add(1)
		procRule := procRules[i]
		go processImageRuleAsync(ImageOptions{
			Image:         image,
			Data:          &imageFile,
			ProcRule:      procRule,
			HeightLimited: heightLimited,
			TargetPath:    config.TargetPath,
		}, channel, &wg)
	}

	go func() {
		wg.Wait()
		close(channel)
	}()

	variants := make([]ProcessedImageVariant, 0, len(procRules))

	for result := range channel {
		variants = append(variants, result)
	}

	result := ImageProcessResult{
		ImageID:     image.ID,
		Name:        image.Name,
		Title:       image.Title,
		Description: image.Description,
		Related:     image.relatedImageIds(),
		Categories:  image.categoryIds(),
		Author:      image.AuthorID,
		Nsfw:        image.Nsfw,
		Variants:    variants,
	}

	if config.ProcessOriginal {
		original, _ := processImageRule(ImageOptions{
			Image:         image,
			Data:          &imageFile,
			ProcRule:      originalProcessingRule(),
			HeightLimited: heightLimited,
		})

		result.Original = original
	}

	logger.Infof("Processed image \"%s\"", image.Name)

	return &result, nil
}

func imageSizeFromBytes(data *[]byte) (bimg.ImageSize, error) {
	return imageSize(bimg.NewImage(*data))
}

func imageSize(image *bimg.Image) (bimg.ImageSize, error) {
	size, err := image.Size()
	if err != nil {
		logger.Errorf("Error determining image size: %v", err)
	}
	return size, err
}

func processFavicon(image *Image) ([]*FaviconProcessResult, error) {
	result, err := processImage(&ImageProcessConfig{
		TargetPath:   appConfig.FaviconDir,
		Image:        image,
		ProcessRules: faviconProcessRules(),
	})

	if err != nil {
		return nil, err
	}

	iconTypes := map[string][]ProcessedImageVariant{}

	for _, variant := range result.Variants {
		iconType := variant.Name

		// Remove name from variant as it's not needed in the process after sorting
		variant.Name = ""

		variants, found := iconTypes[iconType]
		if !found {
			variants = make([]ProcessedImageVariant, 0)
		}

		variants = append(variants, variant)
		iconTypes[iconType] = variants
	}

	processResults := make([]*FaviconProcessResult, 0, len(iconTypes))

	for name, variants := range iconTypes {
		processResults = append(processResults, &FaviconProcessResult{
			Name:     name,
			Author:   image.AuthorID,
			Variants: variants,
		})
	}

	return processResults, nil
}

func processImageRuleAsync(options ImageOptions, targetChannel chan<- ProcessedImageVariant, wg *sync.WaitGroup) {
	defer wg.Done()
	result, err := processImageRule(options)
	if err == nil && result != nil {
		targetChannel <- *result
	}
}

func processImageRule(imageOptions ImageOptions) (*ProcessedImageVariant, error) {
	procRule := imageOptions.ProcRule

	options := bimg.Options{
		Type:    procRule.Format,
		Quality: procRule.Quality,
		Enlarge: procRule.Enlarge,
	}

	if procRule.Width > 0 && procRule.Height > 0 {
		options.Crop = true
		options.Gravity = bimg.GravitySmart
		options.Height = procRule.Height
		options.Width = procRule.Width
	} else {
		if imageOptions.HeightLimited {
			options.Height = procRule.MaxDim
		} else {
			options.Width = procRule.MaxDim
		}
	}

	if procRule.Background != nil {
		options.Background = *procRule.Background
	}

	processed, err := bimg.NewImage(*imageOptions.Data).Process(options)
	if err != nil {
		logger.Errorf("Error processing image: %v", err)
		return nil, err
	}

	size, err := imageSizeFromBytes(&processed)
	if err != nil {
		return nil, err
	}

	result := ProcessedImageVariant{
		Width:   size.Width,
		Height:  size.Height,
		Quality: procRule.Quality,
		Suffix:  procRule.Suffix,
		Format:  bimg.ImageTypeName(procRule.Format),
	}

	baseFileName := imageOptions.Image.ImageIdentifier()
	if len(procRule.Name) > 0 {
		baseFileName = procRule.Name
	}

	if len(procRule.Suffix) > 0 {
		result.FileName = fmt.Sprintf("%s-%s.%s", baseFileName, procRule.Suffix, bimg.ImageTypeName(procRule.Format))
	} else if procRule.NoSizeSuffix {
		result.FileName = fmt.Sprintf("%s.%s", baseFileName, bimg.ImageTypeName(procRule.Format))
	} else {
		result.FileName = processedImageFilename(baseFileName, size, procRule.Format)
	}

	result.Name = baseFileName

	targetPath := imageOptions.TargetPath
	if targetPath == "" {
		targetPath = appConfig.ProcessedDir
	}

	targetPath = path.Join(targetPath, result.FileName)

	err = os.WriteFile(targetPath, processed, 0666)
	if err != nil {
		logger.Errorf("Error writing processed image: %v", err)
		return nil, err
	}

	return &result, nil
}

func processedImageFilename(name string, size bimg.ImageSize, format bimg.ImageType) string {
	return fmt.Sprintf("%s-%dx%d.%s", name, size.Width, size.Height, bimg.ImageTypeName(format))
}
