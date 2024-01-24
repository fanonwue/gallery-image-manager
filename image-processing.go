package main

import (
	"fmt"
	"github.com/h2non/bimg"
	"math"
	"os"
	"path"
	"sync"
)

type (
	ProcessingRule struct {
		Quality int
		MaxDim  int
		Height  int
		Width   int
		Suffix  string
		Enlarge bool
		Format  bimg.ImageType
	}

	ImageOptions struct {
		Image         *Image
		Data          *[]byte
		ProcRule      ProcessingRule
		HeightLimited bool
	}

	ImageProcessResult struct {
		ImageID  uint           `json:"imageId" yaml:"imageId"`
		Original *ImageVariant  `json:"original" yaml:"original"`
		Variants []ImageVariant `json:"variants" yaml:"variants"`
	}

	ImageVariant struct {
		Height   int    `json:"height" yaml:"height"`
		Width    int    `json:"width" yaml:"width"`
		Format   string `json:"format" yaml:"format"`
		FileName string `json:"fileName" yaml:"fileName"`
		Quality  int    `json:"quality" yaml:"quality"`
	}
)

const (
	defaultImageFormat   = bimg.WEBP
	defaultImageQuality  = 75
	highImageQuality     = 90
	originalImageQuality = 95
)

var (
	defaultProcRules []ProcessingRule
	originalProcRule *ProcessingRule
)

func defaultProcessingRules() []ProcessingRule {
	if defaultProcRules == nil {

		metaMaxHeight := 1000
		metaMaxWidth := int(math.Round(float64(metaMaxHeight) * 1.91))

		defaultProcRules = []ProcessingRule{
			ProcessingRule{
				Quality: defaultImageQuality,
				MaxDim:  900,
				Format:  defaultImageFormat,
			},
			ProcessingRule{
				Quality: defaultImageQuality,
				MaxDim:  1200,
				Format:  defaultImageFormat,
			},
			ProcessingRule{
				Quality: defaultImageQuality,
				MaxDim:  2400,
				Format:  defaultImageFormat,
			},
			ProcessingRule{
				Quality: highImageQuality,
				MaxDim:  3000,
				Format:  defaultImageFormat,
			},
			ProcessingRule{
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
			Quality: originalImageQuality,
			Format:  defaultImageFormat,
			MaxDim:  3000,
		}
	}

	return *originalProcRule
}

func processImageAsync(image *Image, targetChannel chan<- *ImageProcessResult, wg *sync.WaitGroup) {
	defer wg.Done()
	result, err := processImage(image)
	if err == nil && result != nil {
		targetChannel <- result
	}
}

func processImage(image *Image) (*ImageProcessResult, error) {
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
	procRules := defaultProcessingRules()
	channel := make(chan ImageVariant, len(procRules))

	for i := range procRules {
		wg.Add(1)
		procRule := procRules[i]
		go processImageRuleAsync(ImageOptions{
			Image:         image,
			Data:          &imageFile,
			ProcRule:      procRule,
			HeightLimited: heightLimited,
		}, channel, &wg)
	}

	go func() {
		wg.Wait()
		close(channel)
	}()

	variants := make([]ImageVariant, 0, len(procRules))

	for result := range channel {
		variants = append(variants, result)
	}

	logger.Infof("Processed image \"%s\"", image.Name)

	original, err := processImageRule(ImageOptions{
		Image:         image,
		Data:          &imageFile,
		ProcRule:      originalProcessingRule(),
		HeightLimited: heightLimited,
	})

	result := ImageProcessResult{
		ImageID:  image.ID,
		Original: original,
		Variants: variants,
	}

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

func processImageRuleAsync(options ImageOptions, targetChannel chan<- ImageVariant, wg *sync.WaitGroup) {
	defer wg.Done()
	result, err := processImageRule(options)
	if err == nil && result != nil {
		targetChannel <- *result
	}
}

func processImageRule(imageOptions ImageOptions) (*ImageVariant, error) {
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

	processed, err := bimg.NewImage(*imageOptions.Data).Process(options)
	if err != nil {
		logger.Errorf("Error processing image: %v", err)
		return nil, err
	}

	size, err := imageSizeFromBytes(&processed)
	if err != nil {
		return nil, err
	}

	result := ImageVariant{
		Width:   size.Width,
		Height:  size.Height,
		Quality: procRule.Quality,
		Format:  bimg.ImageTypeName(procRule.Format),
	}

	if len(procRule.Suffix) > 0 {
		result.FileName = fmt.Sprintf("%s-%s.%s", imageOptions.Image.ImageIdentifier(), procRule.Suffix, bimg.ImageTypeName(procRule.Format))
	} else {
		result.FileName = processedImageFilename(imageOptions.Image.ImageIdentifier(), size, procRule.Format)
	}

	targetPath := path.Join(processedImageDir, result.FileName)

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
