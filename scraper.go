package main

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strconv"
	"strings"

	"github.com/go-redis/redis"
	"github.com/gocolly/colly"
	"github.com/joho/godotenv"
)

type Target struct {
	url          string
	run          func(string, string, int) string
	scrapeTarget string
	targetSize   int
}

type returnResult struct {
	changesFound bool
	changesOn    string
}

func db() *redis.Client {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	return redis.NewClient(&redis.Options{
		Addr:     os.Getenv("DB_ADDR"),
		Password: os.Getenv("DB_PASSWORD"),
		DB:       0,
	})
}

func scraper(url, scrapeTarget string) <-chan []string {
	availableChannel := make(chan []string)
	go func() {
		availableAppsSlice := []string{}
		allowedDomain := strings.Split(url, "/")[2]

		c := colly.NewCollector(
			colly.AllowedDomains(allowedDomain),
		)

		c.OnHTML(scrapeTarget, func(e *colly.HTMLElement) {
			var availableApp string = strings.TrimSpace(e.Text)
			availableAppsSlice = append(availableAppsSlice, strings.Join(strings.Fields(availableApp), " "))

		})

		c.OnScraped(func(*colly.Response) {
			availableChannel <- availableAppsSlice
		})

		c.Visit(url)
	}()
	return availableChannel
}

var targetMap = map[string]Target{
	"finfast": {url: "https://finfast.se/lediga-objekt", scrapeTarget: ".title", targetSize: 4, run: func(url, scrapeTarget string, targetSize int) string {
		scrapedData := <-scraper(url, scrapeTarget)
		availableAppsWithinTargetSize := []string{}
		for _, item := range scrapedData {
			sizeStr := strings.Split(item, " ")[0]
			sizeInt, _ := strconv.Atoi(sizeStr)
			if sizeInt == targetSize {
				availableAppsWithinTargetSize = append(availableAppsWithinTargetSize, item)
			}
		}
		return strings.Join(availableAppsWithinTargetSize, ",")
	}},

	"lundbergs": {url: "https://www.lundbergsfastigheter.se/bostad/lediga-lagenheter/orebro", scrapeTarget: ".closed", targetSize: 3, run: func(url, scrapeTarget string, targetSize int) string {
		scrapedData := <-scraper(url, scrapeTarget)
		availableAppsWithinTargetSize := []string{}
		for _, item := range scrapedData {
			if strings.Contains(item, strconv.Itoa(targetSize)+" rum och kök") {
				availableAppsWithinTargetSize = append(availableAppsWithinTargetSize, item)
			}
		}
		return strings.Join(availableAppsWithinTargetSize, ",")
	}},
}

func sendMail(changesOn string) {

	envErr := godotenv.Load()
	if envErr != nil {
		log.Fatal("Error loading .env file")
	}
	from := os.Getenv("EMAIL_USERNAME")
	password := os.Getenv("EMAIL_PASSWORD")

	to := []string{
		os.Getenv("EMAIL_USERNAME"),
	}

	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	message := []byte("new apps found on " + changesOn)

	auth := smtp.PlainAuth("", from, password, smtpHost)

	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, from, to, message)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("New apps found, email sent successfull")
}

func getAppartments() returnResult {
	availableApps := map[string]string{}
	returnResult := returnResult{}

	for targetName, target := range targetMap {
		targetData := target.run(target.url, target.scrapeTarget, target.targetSize)
		if len(targetData) > 0 {
			availableApps[targetName] = targetData
		}
	}

	for key, scrapedValue := range availableApps {
		dbValue, err := db().Get(key).Result()
		if err == redis.Nil {
			db().Set(key, scrapedValue, 0)
			returnResult.changesFound = true
			returnResult.changesOn += " " + key
			return returnResult
		}
		if dbValue != scrapedValue {
			if !returnResult.changesFound {
				returnResult.changesFound = true
			}
			db().Set(key, scrapedValue, 0)
			returnResult.changesOn += " " + key
		}

	}

	return returnResult
}

func main() {
	result := getAppartments()
	if result.changesFound {
		sendMail(result.changesOn)
	} else {
		fmt.Println("no new data found")
	}
}
