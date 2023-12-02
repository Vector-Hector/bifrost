package main

import (
	"fmt"
	util "github.com/Vector-Hector/goutil"
	"github.com/artonge/go-gtfs"
	"github.com/blevesearch/bleve"
	"strconv"
	"time"
)

func GetStopByName(index bleve.Index, query string) uint32 {
	search := bleve.NewSearchRequest(bleve.NewMatchQuery(query))
	searchResult, err := index.Search(search)
	if err != nil {
		panic(err)
	}

	if len(searchResult.Hits) == 0 {
		return 0
	}

	id, err := strconv.Atoi(searchResult.Hits[0].ID)
	if err != nil {
		panic(err)
	}

	return uint32(id)
}

type IndexStop struct {
	ID          string
	Name        string
	Description string
}

func CreateIndex(fileName string, feed *gtfs.GTFS) bleve.Index {
	mapping := bleve.NewIndexMapping()

	index, err := bleve.New(fileName, mapping)
	if err != nil {
		panic(err)
	}

	t := time.Now()

	for i, stop := range feed.Stops {
		if i%100 == 0 {
			fmt.Println(i, "/", len(feed.Stops))
			util.PrintJSON(stop)
		}

		err = index.Index(strconv.Itoa(i), IndexStop{
			ID:          stop.ID,
			Name:        stop.Name,
			Description: stop.Description,
		})
		if err != nil {
			panic(err)
		}
	}

	fmt.Println("Created index in", time.Since(t))
	return index
}
