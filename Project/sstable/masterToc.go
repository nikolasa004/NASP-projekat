package sstable

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
)

// pravim master TOC fajl, u njemu se nalaze spisak svih postojeci sstabela

func UpdateMasterTOC(sstableDir string) error {
	file, err := os.OpenFile("project/data/sstable/master-toc.txt", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
		return err
	}
	defer file.Close()

	_, err = file.WriteString(sstableDir + "\n")
	if err != nil {
		log.Fatal(err)
		return err
	}

	return nil
}

// funkcija koja cita master TOC fajl i vraca sve sstabele
func ReadMasterTOC() ([]string, error) {
	file, err := os.Open("project/data/sstable/master-toc.txt")
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	defer file.Close()

	var sstables []string
	var sstable string

	for {
		_, err := fmt.Fscan(file, &sstable)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
			return nil, err
		}
		sstables = append(sstables, sstable)
	}

	return sstables, nil
}

func GetNextTableID() (int, error) {
	file, err := os.Open("project/data/sstable/master-toc.txt")
	if os.IsNotExist(err) {
		return 1, nil
	} else if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	maxID := 0

	for scanner.Scan() {
		line := scanner.Text()
		var id int
		_, err := fmt.Sscanf(line, "sstable-%04d", &id)
		if err == nil && id > maxID {
			maxID = id
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return maxID + 1, nil
}
