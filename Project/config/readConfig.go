package config

import (
	"encoding/json"
	"log"
	"os"
)

// pravi se struktura koja odgovara json fajlu
type Config struct {
	BlockManager struct {
		BlocksInSegment int `json:"blocksInSegment"`
		BlockSize       int `json:"blockSize"`
		CacheSize       int `json:"cacheSize"`
	} `json:"BlockManager"`

	Memtable struct {
		ImplementType string `json:"implementType"`
		NumOfTables   int    `json:"numOfTables"`
		TableSize     int    `json:"tableSize"`
	} `json:"Memtable"`

	SSTableManager struct {
		SummarySampleRate int  `json:"summarySampleRate"`
		OneFile           bool `json:"oneFile"`
		Compresion        bool `json:"compresion"`
	} `json:"SSTableManager"`
}

func ReadConfig(filePath string) (Config, error) {
	var config Config

	file, err := os.ReadFile(filePath)

	if err != nil {
		log.Fatal(err)
		return config, err
	}

	json.Unmarshal(file, &config) //parsira json fajl i smesta ga u strukturu

	return config, nil
}
