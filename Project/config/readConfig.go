package config

import (
	"encoding/json"
	"log"
	"os"
)

var filePath = "config/config.json" //putanja do konfiguracionog fajla

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

func ReadConfig() Config { //cita konfiguracioni fajl i vraca strukturu Config
	var config Config

	file, err := os.ReadFile(filePath)

	if err != nil {
		log.Print("Greska pri citanju konfiguracionog fajla")

		if os.IsNotExist(err) {
			log.Print("Konfiguracioni fajl ne postoji, pokusaj pravljenja novog sa podrazumevanim vrednostima")
			config = WriteConfig() // ako fajl ne postoji, pravi novi sa podrazumevanim vrednostima
			return config
		} else {
			log.Print("Greska pri citanju konfiguracionog fajla, nastavljamo sa podrazumevanim vrednostima")
			return GetDefaultConfig() // ako postoji greska pri citanju, vraca podrazumevane vrednosti
		}
	}

	json.Unmarshal(file, &config) //parsira json fajl i smesta ga u strukturu

	return config
}

func WriteConfig() Config { //pravi novi konfiguracioni fajl
	config := GetDefaultConfig()

	file, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Print("Greska pri kreiranju novog konfiguracionog fajla, nastavljamo sa podrazumevanim vrednostima")
		return config
	}

	err = os.WriteFile(filePath, file, 0644)
	if err != nil {
		log.Print("Greska pri pisanju konfiguracionog fajla, nastavljamo sa podrazumevanim vrednostima")
		return config
	}

	log.Print("Novi konfiguracioni fajl je kreiran sa podrazumevanim vrednostima")

	return config
}

func GetDefaultConfig() Config { // vraca podrazumevane vrednosti konfiguracije
	config := Config{
		BlockManager: struct {
			BlocksInSegment int `json:"blocksInSegment"`
			BlockSize       int `json:"blockSize"`
			CacheSize       int `json:"cacheSize"`
		}{
			BlocksInSegment: 10,
			BlockSize:       1024,
			CacheSize:       10,
		},
		Memtable: struct {
			ImplementType string `json:"implementType"`
			NumOfTables   int    `json:"numOfTables"`
			TableSize     int    `json:"tableSize"`
		}{
			ImplementType: "SkipList",
			NumOfTables:   10,
			TableSize:     10,
		},
		SSTableManager: struct {
			SummarySampleRate int  `json:"summarySampleRate"`
			OneFile           bool `json:"oneFile"`
			Compresion        bool `json:"compresion"`
		}{
			SummarySampleRate: 10,
			OneFile:           true,
			Compresion:        false,
		},
	}

	return config
}
