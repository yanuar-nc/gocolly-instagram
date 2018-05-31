package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gocolly/colly"
)

// found in https://www.instagram.com/static/bundles/en_US_Commons.js/68e7390c5938.js
// included from profile page
const instagramQueryId = 17888483320059182

// "id": user id, "after": end cursor
const nextPageURL string = `https://www.instagram.com/graphql/query/?query_id=%v&variables=%v`
const nextPagePayload string = `{"id":"%s","first":15,"after":"%s"}`

var requestID string

type pageInfo struct {
	EndCursor string `json:"end_cursor"`
	NextPage  bool   `json:"has_next_page"`
}

type mainPageData struct {
	Rhxgis    string `json:"rhx_gis"`
	EntryData struct {
		ProfilePage []struct {
			Graphql struct {
				User struct {
					Id       string `json:"id"`
					Follower struct {
						Count int `json:"count"`
					} `json:"edge_followed_by"`
					Following struct {
						Count int `json:"count"`
					} `json:"edge_follow"`
					IsPrivate  bool   `json:"is_private"`
					IsVerified bool   `json:"is_verified"`
					Username   string `json:"username"`
					Fullname   string `json:"full_name"`
					Biography  string `json:"biography"`
					Media      struct {
						Edges []struct {
							Node struct {
								ImageURL     string `json:"display_url"`
								ThumbnailURL string `json:"thumbnail_src"`
								IsVideo      bool   `json:"is_video"`
								Date         int    `json:"date"`
								Dimensions   struct {
									Width  int `json:"width"`
									Height int `json:"height"`
								} `json:"dimensions"`
							} `json::node"`
						} `json:"edges"`
						PageInfo pageInfo `json:"page_info"`
					} `json:"edge_owner_to_timeline_media"`
				} `json:"user"`
			} `json:"graphql"`
		} `json:"ProfilePage"`
	} `json:"entry_data"`
}

type nextPageData struct {
	Data struct {
		User struct {
			Container struct {
				PageInfo pageInfo `json:"page_info"`
				Edges    []struct {
					Node struct {
						ImageURL     string `json:"display_url"`
						ThumbnailURL string `json:"thumbnail_src"`
						IsVideo      bool   `json:"is_video"`
						Date         int    `json:"taken_at_timestamp"`
						Dimensions   struct {
							Width  int `json:"width"`
							Height int `json:"height"`
						}
					}
				} `json:"edges"`
			} `json:"edge_owner_to_timeline_media"`
		}
	} `json:"data"`
}

func main() {
	if len(os.Args) != 2 {
		log.Println("Missing account name argument")
		os.Exit(1)
	}

	var actualUserId string
	instagramAccount := os.Args[1]
	outputDir := fmt.Sprintf("./instagram_%s/", instagramAccount)

	c := colly.NewCollector(
		//colly.CacheDir("./_instagram_cache/"),
		colly.UserAgent("Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36"),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("X-Requested-With", "XMLHttpRequest")
		r.Headers.Set("Referrer", "https://www.instagram.com/"+instagramAccount)
		if r.Ctx.Get("gis") != "" {
			gis := fmt.Sprintf("%s:%s", r.Ctx.Get("gis"), r.Ctx.Get("variables"))
			h := md5.New()
			h.Write([]byte(gis))
			gisHash := fmt.Sprintf("%x", h.Sum(nil))
			r.Headers.Set("X-Instagram-GIS", gisHash)
		}
	})

	c.OnHTML("html", func(e *colly.HTMLElement) {
		d := c.Clone()
		d.OnResponse(func(r *colly.Response) {
			idStart := bytes.Index(r.Body, []byte(`:n},queryId:"`))
			requestID = string(r.Body[idStart+13 : idStart+45])
		})
		requestIDURL := e.Request.AbsoluteURL(e.ChildAttr(`link[as="script"]`, "href"))

		d.Visit(requestIDURL)

		dat := e.ChildText("body > script:first-of-type")
		jsonData := dat[strings.Index(dat, "{") : len(dat)-1]
		data := &mainPageData{}
		err := json.Unmarshal([]byte(jsonData), data)
		if err != nil {
			log.Fatal(err)
			fmt.Println("Fatal")
		}

		page := data.EntryData.ProfilePage[0]
		user := page.Graphql.User
		if user.IsPrivate {
			fmt.Println("Account `" + user.Username + "` is private, can not be proceed")
			return
		}

		log.Println("saving output to ", outputDir)
		os.MkdirAll(outputDir, os.ModePerm)

		actualUserId = user.Id
		for _, obj := range page.Graphql.User.Media.Edges {
			// skip videos
			if obj.Node.IsVideo {
				continue
			}
			c.Visit(obj.Node.ImageURL)
		}
		nextPageVars := fmt.Sprintf(nextPagePayload, actualUserId, page.Graphql.User.Media.PageInfo.EndCursor)
		e.Request.Ctx.Put("variables", nextPageVars)
		if page.Graphql.User.Media.PageInfo.NextPage {
			u := fmt.Sprintf(
				nextPageURL,
				instagramQueryId,
				nextPageVars,
			)
			// spew.Dump(u, nextPageVars)
			log.Println("HTML: Next page found", u)
			e.Request.Ctx.Put("gis", data.Rhxgis)

			e.Request.Visit(u)
		}
	})

	c.OnError(func(r *colly.Response, e error) {
		log.Println("error:", e, r.Request.URL, string(r.Body))
	})

	c.OnResponse(func(r *colly.Response) {

		if strings.Index(r.Headers.Get("Content-Type"), "image") > -1 {
			r.Save(outputDir + r.FileName())
			fmt.Println("Image is successfully saved: " + outputDir + r.FileName())
			return
		}

		if strings.Index(r.Headers.Get("Content-Type"), "json") == -1 {
			return
		}

		data := &nextPageData{}
		err := json.Unmarshal(r.Body, data)
		if err != nil {
			log.Fatal(err)
		}

		for _, obj := range data.Data.User.Container.Edges {
			// skip videos
			if obj.Node.IsVideo {
				continue
			}
			c.Visit(obj.Node.ImageURL)
		}
		if data.Data.User.Container.PageInfo.NextPage {
			nextPageVars := fmt.Sprintf(nextPagePayload, actualUserId, data.Data.User.Container.PageInfo.EndCursor)
			r.Request.Ctx.Put("variables", nextPageVars)
			u := fmt.Sprintf(
				nextPageURL,
				instagramQueryId,
				nextPageVars,
			)
			log.Println("Response: Next page found", u)
			r.Request.Visit(u)
		}
	})

	c.Visit("https://instagram.com/" + instagramAccount)
}
