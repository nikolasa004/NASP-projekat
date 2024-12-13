package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/config"
)

func main() {
	// bira se neka od metoda GET, PUT, DELETE
	// od svake operacije se pravi zapis

	config, err := config.ReadConfig("config/config.json")

	blockSize := 1024
	blocksPerSegment := 10
	cacheSize := 10

	if err != nil {
		fmt.Println("Greska pri citanju konfiguracionog fajla")
	} else {
		blockSize = config.BlockManager.BlockSize
		blocksPerSegment = config.BlockManager.BlocksInSegment
		cacheSize = config.BlockManager.CacheSize
	}

	walPath := "BlockManager/wal"
	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, cacheSize, walPath)
	blockManager.Reconstruct()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("Unesite operaciju ili / za kraj: ")
		var operacija string
		scanner.Scan()
		operacija = scanner.Text()

		if operacija == "GET" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			//kljuc := scanner.Text()
			//za sad bez memtable-a i sstable-a ne moze da se vrati vrednost
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
		} else if operacija == "DELETE" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()

			//napraviti zapis sa tombstone = true
			records := BlockManager.CreateBinaryRecordsWithFragmentation(true, kljuc, []byte(""), blockSize)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
		} else if operacija == "/" {
			break
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
