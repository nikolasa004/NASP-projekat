package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/config"
)


func main() {
	// bira se neka od metoda GET, PUT, DELETE
	// od svake operacije se pravi zapis

	config, err := config.ReadConfig("config/config.json")

	//WAL
	blockSize := 1024
	blocksPerSegment := 10
	cacheSize := 10

	//Memtable
	implementType := "SkipList"
	numOfTables := 3
	tableSize := 10

	if err != nil {
		fmt.Println("Greska pri citanju konfiguracionog fajla")
	} else {
		blockSize = config.BlockManager.BlockSize
		blocksPerSegment = config.BlockManager.BlocksInSegment
		cacheSize = config.BlockManager.CacheSize
		implementType = config.Memtable.ImplementType
		numOfTables = config.Memtable.NumOfTables
		tableSize = config.Memtable.TableSize
	}

	walPath := "BlockManager/wal"
	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, cacheSize, walPath)
	blockManager.Reconstruct()

	memtable := Memtable.NewMemtable(implementType, numOfTables, tableSize)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("Unesite operaciju ili / za kraj: ")
		var operacija string
		scanner.Scan()
		operacija = scanner.Text()

		if operacija == "GET" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()
			record := memtable.GetRecord(kljuc)
			if record == nil {
				//proveriti u SStable-u kada se implementira
			}
			fmt.Println(record)
		} else if operacija == "PUT" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()

			fmt.Println("Unesite vrednost: ")
			scanner.Scan()
			vrednost := scanner.Text()

			records := BlockManager.CreateBinaryRecordsWithFragmentation(false, kljuc, []byte(vrednost), blockSize)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			record := memtable.CreateRecord(false, kljuc, []byte(vrednost))
			memtable.PutRecord(record)
		} else if operacija == "DELETE" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()

			//napraviti zapis sa tombstone = true
			records := BlockManager.CreateBinaryRecordsWithFragmentation(true, kljuc, []byte(""), blockSize)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			record := memtable.CreateRecord(true,kljuc, []byte(""))
			memtable.PutRecord(record)
		} else if operacija == "/" {
			break
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
