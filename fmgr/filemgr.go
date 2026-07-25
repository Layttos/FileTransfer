package fmgr

import (
	"fmt"
	"os"
)

func CheckExist(path string) bool {
	_, err := os.Stat(path)
	return err != nil
}

func CreateNewFile(path string, psw string) {
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprint(os.Stderr, "An error occured while attemping to create a new file")
		return
	}
	defer file.Close()
}

func ChangeFileID(oldID string, newID string) error {
	oldPath := "./files/" + oldID
	newPath := "./files/" + newID

	err := os.Rename(oldPath, newPath)
	if err != nil {
		return fmt.Errorf("An error occurred while trying to change the file ID: %v", err)
	}

	return nil
}

func DeleteFile(path string) error {
	err := os.Remove(path)
	if err != nil {
		return fmt.Errorf("An error occurred while trying to delete the file: %v", err)
	}

	return nil
}

func RenameFile(oldPath string, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return fmt.Errorf("An error occurred while trying to rename the file: %v", err)
	}

	return nil
}
