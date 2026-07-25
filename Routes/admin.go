package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

var RenameBody struct {
}

type rqBody struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	Username   string `json:"username"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	InviteCode string `json:"inviteCode"`
	Action     string `json:"action"`
	FileID     string `json:"fileID"`
	NewName    string `json:"newFileName"`
	NewFileID  string `json:"newFileID"`
}

func HandleAdminLogin(w http.ResponseWriter, req *http.Request) {

	admin_username, err := req.Cookie("admin_username")
	admin_password, err1 := req.Cookie("admin_password")
	if (err != nil && err1 == nil) || (err == nil && err1 != nil) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(`{"message": "USER_OR_PASSWORD_INCORRECT"}`)
	}

	if admin_username != nil && admin_password != nil {
		if postsql.AdminCheckCredentials(admin_username.Value, admin_password.Value) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(`{"message": "USER_ALREADY_LOGGED_IN"}`)
			http.ServeFile(w, req, "./public/admin/administration.html")
			return
		}
	}

	http.ServeFile(w, req, "./public/admin/login.html")
	return

}

func HandleAdminRegister(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "./public/admin/signin.html")
}

func HandleAdmin(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, "/admin/login", http.StatusSeeOther)
	return
}

func HandleAdminAPI(w http.ResponseWriter, req *http.Request) {
	if len(req.URL.Query()) != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(`{"message": "TOO MANY ARGUMENTS"}`)
		return
	}

	var payload rqBody

	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		json.NewEncoder(w).Encode(`{"message": "The request body is not valid JSON"}`)
		return
	}

	fileID := req.URL.Query().Get("id")
	fullPath := filepath.Join(`./files/`, fileID, postsql.GetFileName(fileID))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	switch payload.Action {
	case "delete":
		{

			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if !postsql.Exists(fileID) {
				json.NewEncoder(w).Encode(`{"message":"The ID you provided wasn't found'"}`)
				return

			}

			postsql.DeleteFile(payload.FileID)
			json.NewEncoder(w).Encode(`{"message": "Deleted file ID ''` + payload.FileID + `'"}`)
			break
		}

	case "rename":
		{
			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if !postsql.Exists(fileID) {
				json.NewEncoder(w).Encode(`{"message":"The ID you provided wasn't found'"}`)
				return

			}

			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				json.NewEncoder(w).Encode(`{"message": "Somehow the ID was found but not the file"}`)
				return
			}

			if postsql.RenameFile(fileID, payload.NewName) {
				json.NewEncoder(w).Encode(`{"message": "FILE_RENAMED_SUCCESSFULLY"}`)
			} else {
				json.NewEncoder(w).Encode(`{"message": "FAILED_TO_RENAME_FILE"}`)
			}
			return

		}
	case "change_id":
		{
			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if !postsql.Exists(fileID) {
				json.NewEncoder(w).Encode(`{"message":"The ID you provided wasn't found'"}`)
				return

			}

			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				json.NewEncoder(w).Encode(`{"message": "Somehow the ID was found but not the file"}`)
				return
			}

			if postsql.ChangeFileID(fileID, payload.NewFileID) {
				json.NewEncoder(w).Encode(`{"message": "FILE_ID_CHANGED_SUCCESSFULLY"}`)
			} else {
				json.NewEncoder(w).Encode(`{"message": "FAILED_TO_CHANGE_FILE_ID"}`)
			}
			return

		}
	case "register":
		{
			if payload.Email == "" || payload.Password == "" || payload.Username == "" || payload.FirstName == "" || payload.LastName == "" || payload.InviteCode == "" {
				json.NewEncoder(w).Encode(`{"message": "ONE_OR_MORE_FIELDS_ARE_EMPTY"}`)
				return
			}

			if postsql.AdminCheckUserExistence(payload.Username) {
				json.NewEncoder(w).Encode(`{"message": "USERNAME_OR_EMAIL_ALREADY_EXISTS"}`)
				return
			}

			if !postsql.AdminRegisterUser(payload.FirstName, payload.LastName, payload.Username, payload.Email, payload.Password, payload.InviteCode) {
				fmt.Println("ça a planté")
				json.NewEncoder(w).Encode(`{"message": "USER_CREATION_FAILED"}`)
				return
			}
			fmt.Println("User created successfully")
			json.NewEncoder(w).Encode(`{"message": "USER_CREATED_SUCCESSFULLY"}`)
			return

		}
	default:
		{
			json.NewEncoder(w).Encode(`{"message": "The action you provided is not valid or empty"}`)
		}
		return
	}

}
