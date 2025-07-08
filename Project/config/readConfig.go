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

	RateLimiter struct {
		Capacity uint32 `json:"capacity"`
		Interval int64  `json:"interval"`
	} `json:"RateLimiter"`
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

	CheckValidConfig(&config) //proverava da li su vrednosti u konfiguraciji validne

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

func CheckValidConfig(config *Config) { //proverava da li su vrednosti u konfiguraciji validne
	if config.BlockManager.BlocksInSegment <= 0 {
		log.Print("Nevalidan broj blokova u segmentu, postavljanje na podrazumevanu vrednost 10")
		config.BlockManager.BlocksInSegment = 10
	}

	if config.BlockManager.BlockSize != 4096 && config.BlockManager.BlockSize != 8192 && config.BlockManager.BlockSize != 16384 {
		log.Print("Nevalidna velicina bloka, postavljanje na podrazumevanu vrednost 4096")
		config.BlockManager.BlockSize = 4096 // 4KB
	}

	if config.BlockManager.CacheSize <= 0 {
		log.Print("Nevalidna velicina blok keša, postavljanje na podrazumevanu vrednost 10")
		config.BlockManager.CacheSize = 10
	}

	if config.Memtable.ImplementType != "SkipList" && config.Memtable.ImplementType != "Hashmap" && config.Memtable.ImplementType != "BTree" {
		log.Print("Nevalidan tip implementacije Memtable, postavljanje na podrazumevanu vrednost 'SkipList'")
		config.Memtable.ImplementType = "SkipList"
	}

	if config.Memtable.NumOfTables <= 0 {
		log.Print("Nevalidan broj tabela u Memtable, postavljanje na podrazumevanu vrednost 10")
		config.Memtable.NumOfTables = 10
	}

	if config.Memtable.TableSize <= 0 {
		log.Print("Nevalidna velicina tabele u Memtable, postavljanje na podrazumevanu vrednost 10")
		config.Memtable.TableSize = 10
	}

	if config.SSTableManager.SummarySampleRate <= 0 {
		log.Print("Nevalidna stopa uzorka za index summary, postavljanje na podrazumevanu vrednost 10")
		config.SSTableManager.SummarySampleRate = 10
	}
}

func GetDefaultConfig() Config { // vraca podrazumevane vrednosti konfiguracije
	config := Config{
		BlockManager: struct {
			BlocksInSegment int `json:"blocksInSegment"`
			BlockSize       int `json:"blockSize"`
			CacheSize       int `json:"cacheSize"`
		}{
			BlocksInSegment: 10,
			BlockSize:       4096,
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

		RateLimiter: struct {
			Capacity uint32 `json:"capacity"`
			Interval int64  `json:"interval"`
		}{
			Capacity: 5,
			Interval: 60,
		},
	}

	return config
}
