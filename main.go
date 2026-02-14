package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed templates/* static/*
var assets embed.FS

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS donors (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	blood_type TEXT NOT NULL,
	phone TEXT,
	city TEXT,
	created_at TEXT NOT NULL,
	deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS recipients (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	blood_type TEXT NOT NULL,
	phone TEXT,
	hospital TEXT,
	created_at TEXT NOT NULL,
	deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS donations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	donor_id INTEGER NOT NULL,
	blood_type TEXT NOT NULL,
	units INTEGER NOT NULL,
	donation_date TEXT NOT NULL,
	expiry_date TEXT NOT NULL,
	deleted_at TEXT,
	FOREIGN KEY(donor_id) REFERENCES donors(id)
);

CREATE TABLE IF NOT EXISTS inventory (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	blood_type TEXT NOT NULL UNIQUE,
	units INTEGER NOT NULL,
	deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS requests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	recipient_id INTEGER NOT NULL,
	blood_type TEXT NOT NULL,
	units INTEGER NOT NULL,
	status TEXT NOT NULL,
	request_date TEXT NOT NULL,
	deleted_at TEXT,
	FOREIGN KEY(recipient_id) REFERENCES recipients(id)
);
`

type Donor struct {
	ID        int
	Name      string
	BloodType string
	Phone     string
	City      string
	CreatedAt string
}

type Recipient struct {
	ID        int
	Name      string
	BloodType string
	Phone     string
	Hospital  string
	CreatedAt string
}

type Donation struct {
	ID           int
	DonorID      int
	DonorName    string
	BloodType    string
	Units        int
	DonationDate string
	ExpiryDate   string
}

type Inventory struct {
	BloodType string
	Units     int
}

type Request struct {
	ID           int
	RecipientID  int
	Recipient    string
	BloodType    string
	Units        int
	Status       string
	RequestDate  string
}

type PageData struct {
	Donors     []Donor
	Recipients []Recipient
	Donations  []Donation
	Inventory  []Inventory
	Requests   []Request
	Message    string
}

func main() {
	db, err := sql.Open("sqlite", "file:bloodbank.db?_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := initDB(db); err != nil {
		log.Fatal(err)
	}

	tmpl := template.Must(template.ParseFS(assets, "templates/index.html"))

	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(assets)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, err := loadPageData(db, "")
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		if err := tmpl.Execute(w, data); err != nil {
			log.Println("template error:", err)
		}
	})

	mux.HandleFunc("/donors", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		bloodType := strings.TrimSpace(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		city := strings.TrimSpace(r.FormValue("city"))
		if name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Donor name and blood type are required.")
			return
		}
		_, err := db.Exec(
			"INSERT INTO donors (name, blood_type, phone, city, created_at) VALUES (?, ?, ?, ?, ?)",
			name, bloodType, phone, city, time.Now().Format("2006-01-02"),
		)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not add donor.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/recipients", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		bloodType := strings.TrimSpace(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		hospital := strings.TrimSpace(r.FormValue("hospital"))
		if name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Recipient name and blood type are required.")
			return
		}
		_, err := db.Exec(
			"INSERT INTO recipients (name, blood_type, phone, hospital, created_at) VALUES (?, ?, ?, ?, ?)",
			name, bloodType, phone, hospital, time.Now().Format("2006-01-02"),
		)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not add recipient.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/donations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		donorID, _ := strconv.Atoi(r.FormValue("donor_id"))
		bloodType := strings.TrimSpace(r.FormValue("blood_type"))
		units, _ := strconv.Atoi(r.FormValue("units"))
		expiry := strings.TrimSpace(r.FormValue("expiry_date"))
		if donorID == 0 || bloodType == "" || units <= 0 || expiry == "" {
			renderWithMessage(w, tmpl, db, "Donation requires donor, blood type, units, and expiry date.")
			return
		}
		_, err := db.Exec(
			"INSERT INTO donations (donor_id, blood_type, units, donation_date, expiry_date) VALUES (?, ?, ?, ?, ?)",
			donorID, bloodType, units, time.Now().Format("2006-01-02"), expiry,
		)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not add donation.")
			return
		}
		if err := upsertInventory(db, bloodType, units); err != nil {
			renderWithMessage(w, tmpl, db, "Donation saved, but inventory update failed.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/donations/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		if id == 0 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		var bloodType string
		var units int
		err := db.QueryRow("SELECT blood_type, units FROM donations WHERE id = ? AND deleted_at IS NULL", id).Scan(&bloodType, &units)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Donation not found.")
			return
		}
		ok, err := consumeInventory(db, bloodType, units)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Inventory update failed.")
			return
		}
		if !ok {
			renderWithMessage(w, tmpl, db, "Cannot delete donation because inventory is already used.")
			return
		}
		_, err = db.Exec("UPDATE donations SET deleted_at = ? WHERE id = ?", time.Now().Format("2006-01-02"), id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not delete donation.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/requests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		recipientID, _ := strconv.Atoi(r.FormValue("recipient_id"))
		bloodType := strings.TrimSpace(r.FormValue("blood_type"))
		units, _ := strconv.Atoi(r.FormValue("units"))
		if recipientID == 0 || bloodType == "" || units <= 0 {
			renderWithMessage(w, tmpl, db, "Request requires recipient, blood type, and units.")
			return
		}
		_, err := db.Exec(
			"INSERT INTO requests (recipient_id, blood_type, units, status, request_date) VALUES (?, ?, ?, ?, ?)",
			recipientID, bloodType, units, "Pending", time.Now().Format("2006-01-02"),
		)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not add request.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/donors/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		name := strings.TrimSpace(r.FormValue("name"))
		bloodType := strings.TrimSpace(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		city := strings.TrimSpace(r.FormValue("city"))
		if id == 0 || name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Donor update requires id, name, and blood type.")
			return
		}
		_, err := db.Exec("UPDATE donors SET name = ?, blood_type = ?, phone = ?, city = ? WHERE id = ?", name, bloodType, phone, city, id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not update donor.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/donors/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		if id == 0 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		_, err := db.Exec("UPDATE donors SET deleted_at = ? WHERE id = ?", time.Now().Format("2006-01-02"), id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not delete donor.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/recipients/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		name := strings.TrimSpace(r.FormValue("name"))
		bloodType := strings.TrimSpace(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		hospital := strings.TrimSpace(r.FormValue("hospital"))
		if id == 0 || name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Recipient update requires id, name, and blood type.")
			return
		}
		_, err := db.Exec("UPDATE recipients SET name = ?, blood_type = ?, phone = ?, hospital = ? WHERE id = ?", name, bloodType, phone, hospital, id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not update recipient.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/recipients/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		if id == 0 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		_, err := db.Exec("UPDATE recipients SET deleted_at = ? WHERE id = ?", time.Now().Format("2006-01-02"), id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not delete recipient.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/fulfill", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		if id == 0 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		var bloodType string
		var units int
		err := db.QueryRow("SELECT blood_type, units FROM requests WHERE id = ?", id).Scan(&bloodType, &units)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Request not found.")
			return
		}
		ok, err := consumeInventory(db, bloodType, units)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Inventory update failed.")
			return
		}
		if !ok {
			renderWithMessage(w, tmpl, db, "Not enough inventory to fulfill request.")
			return
		}
		_, err = db.Exec("UPDATE requests SET status = ? WHERE id = ?", "Fulfilled", id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not update request.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/requests/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		bloodType := strings.TrimSpace(r.FormValue("blood_type"))
		units, _ := strconv.Atoi(r.FormValue("units"))
		status := strings.TrimSpace(r.FormValue("status"))
		if id == 0 || bloodType == "" || units <= 0 || status == "" {
			renderWithMessage(w, tmpl, db, "Request update requires id, blood type, units, and status.")
			return
		}

		var oldBlood string
		var oldUnits int
		var oldStatus string
		err := db.QueryRow("SELECT blood_type, units, status FROM requests WHERE id = ? AND deleted_at IS NULL", id).Scan(&oldBlood, &oldUnits, &oldStatus)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Request not found.")
			return
		}

		if oldStatus == "Fulfilled" {
			if status != "Fulfilled" || oldBlood != bloodType || oldUnits != units {
				renderWithMessage(w, tmpl, db, "Cannot modify a fulfilled request.")
				return
			}
		}

		if oldStatus != "Fulfilled" && status == "Fulfilled" {
			ok, err := consumeInventory(db, bloodType, units)
			if err != nil {
				renderWithMessage(w, tmpl, db, "Inventory update failed.")
				return
			}
			if !ok {
				renderWithMessage(w, tmpl, db, "Not enough inventory to fulfill request.")
				return
			}
		}

		_, err = db.Exec("UPDATE requests SET blood_type = ?, units = ?, status = ? WHERE id = ?", bloodType, units, status, id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not update request.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/requests/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("id"))
		if id == 0 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		var status string
		err := db.QueryRow("SELECT status FROM requests WHERE id = ? AND deleted_at IS NULL", id).Scan(&status)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Request not found.")
			return
		}
		if status == "Fulfilled" {
			renderWithMessage(w, tmpl, db, "Cannot delete a fulfilled request.")
			return
		}
		_, err = db.Exec("UPDATE requests SET status = ?, deleted_at = ? WHERE id = ?", "Cancelled", time.Now().Format("2006-01-02"), id)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not delete request.")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	addr := ":8080"
	log.Println("Blood Bank DBMS running on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func initDB(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	if err := ensureColumn(db, "donors", "deleted_at", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(db, "recipients", "deleted_at", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(db, "donations", "deleted_at", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(db, "inventory", "deleted_at", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(db, "requests", "deleted_at", "TEXT"); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, table string, column string, colType string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if rows.Err() != nil {
		return rows.Err()
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colType))
	return err
}

func renderWithMessage(w http.ResponseWriter, tmpl *template.Template, db *sql.DB, msg string) {
	data, err := loadPageData(db, msg)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		log.Println("template error:", err)
	}
}

func loadPageData(db *sql.DB, msg string) (PageData, error) {
	data := PageData{Message: msg}

	donors, err := loadDonors(db)
	if err != nil {
		return data, err
	}
	data.Donors = donors

	recipients, err := loadRecipients(db)
	if err != nil {
		return data, err
	}
	data.Recipients = recipients

	donations, err := loadDonations(db)
	if err != nil {
		return data, err
	}
	data.Donations = donations

	inventory, err := loadInventory(db)
	if err != nil {
		return data, err
	}
	data.Inventory = inventory

	requests, err := loadRequests(db)
	if err != nil {
		return data, err
	}
	data.Requests = requests

	return data, nil
}

func loadDonors(db *sql.DB) ([]Donor, error) {
	rows, err := db.Query("SELECT id, name, blood_type, phone, city, created_at FROM donors WHERE deleted_at IS NULL ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var donors []Donor
	for rows.Next() {
		var d Donor
		if err := rows.Scan(&d.ID, &d.Name, &d.BloodType, &d.Phone, &d.City, &d.CreatedAt); err != nil {
			return nil, err
		}
		donors = append(donors, d)
	}
	return donors, rows.Err()
}

func loadRecipients(db *sql.DB) ([]Recipient, error) {
	rows, err := db.Query("SELECT id, name, blood_type, phone, hospital, created_at FROM recipients WHERE deleted_at IS NULL ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recipients []Recipient
	for rows.Next() {
		var r Recipient
		if err := rows.Scan(&r.ID, &r.Name, &r.BloodType, &r.Phone, &r.Hospital, &r.CreatedAt); err != nil {
			return nil, err
		}
		recipients = append(recipients, r)
	}
	return recipients, rows.Err()
}

func loadDonations(db *sql.DB) ([]Donation, error) {
	rows, err := db.Query(`
		SELECT d.id, d.donor_id, donors.name, d.blood_type, d.units, d.donation_date, d.expiry_date
		FROM donations d
		JOIN donors ON donors.id = d.donor_id
		WHERE d.deleted_at IS NULL
		ORDER BY d.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var donations []Donation
	for rows.Next() {
		var d Donation
		if err := rows.Scan(&d.ID, &d.DonorID, &d.DonorName, &d.BloodType, &d.Units, &d.DonationDate, &d.ExpiryDate); err != nil {
			return nil, err
		}
		donations = append(donations, d)
	}
	return donations, rows.Err()
}

func loadInventory(db *sql.DB) ([]Inventory, error) {
	rows, err := db.Query("SELECT blood_type, units FROM inventory WHERE deleted_at IS NULL ORDER BY blood_type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var inv []Inventory
	for rows.Next() {
		var i Inventory
		if err := rows.Scan(&i.BloodType, &i.Units); err != nil {
			return nil, err
		}
		inv = append(inv, i)
	}
	return inv, rows.Err()
}

func loadRequests(db *sql.DB) ([]Request, error) {
	rows, err := db.Query(`
		SELECT r.id, r.recipient_id, recipients.name, r.blood_type, r.units, r.status, r.request_date
		FROM requests r
		JOIN recipients ON recipients.id = r.recipient_id
		WHERE r.deleted_at IS NULL
		ORDER BY r.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []Request
	for rows.Next() {
		var r Request
		if err := rows.Scan(&r.ID, &r.RecipientID, &r.Recipient, &r.BloodType, &r.Units, &r.Status, &r.RequestDate); err != nil {
			return nil, err
		}
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

func upsertInventory(db *sql.DB, bloodType string, units int) error {
	res, err := db.Exec("UPDATE inventory SET units = units + ?, deleted_at = NULL WHERE blood_type = ?", units, bloodType)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		_, err = db.Exec("INSERT INTO inventory (blood_type, units, deleted_at) VALUES (?, ?, NULL)", bloodType, units)
		return err
	}
	return nil
}

func consumeInventory(db *sql.DB, bloodType string, units int) (bool, error) {
	var current int
	err := db.QueryRow("SELECT units FROM inventory WHERE blood_type = ? AND deleted_at IS NULL", bloodType).Scan(&current)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current < units {
		return false, nil
	}
	_, err = db.Exec("UPDATE inventory SET units = units - ? WHERE blood_type = ?", units, bloodType)
	if err != nil {
		return false, err
	}
	return true, nil
}
