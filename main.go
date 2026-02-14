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

CREATE TABLE IF NOT EXISTS blood_types (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS donors (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	blood_type_id INTEGER NOT NULL,
	phone TEXT,
	city TEXT,
	created_at TEXT NOT NULL,
	deleted_at TEXT,
	FOREIGN KEY(blood_type_id) REFERENCES blood_types(id)
);

CREATE TABLE IF NOT EXISTS recipients (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	blood_type_id INTEGER NOT NULL,
	phone TEXT,
	hospital TEXT,
	created_at TEXT NOT NULL,
	deleted_at TEXT,
	FOREIGN KEY(blood_type_id) REFERENCES blood_types(id)
);

CREATE TABLE IF NOT EXISTS donations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	donor_id INTEGER NOT NULL,
	units INTEGER NOT NULL,
	donation_date TEXT NOT NULL,
	expiry_date TEXT NOT NULL,
	deleted_at TEXT,
	FOREIGN KEY(donor_id) REFERENCES donors(id)
);

CREATE TABLE IF NOT EXISTS inventory (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	blood_type_id INTEGER NOT NULL UNIQUE,
	units INTEGER NOT NULL,
	deleted_at TEXT,
	FOREIGN KEY(blood_type_id) REFERENCES blood_types(id)
);

CREATE TABLE IF NOT EXISTS requests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	recipient_id INTEGER NOT NULL,
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
		bloodType := normalizeBloodType(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		city := strings.TrimSpace(r.FormValue("city"))
		if name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Donor name and blood type are required.")
			return
		}
		bloodTypeID, err := getOrCreateBloodTypeID(db, bloodType)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not add donor.")
			return
		}
		_, err = db.Exec(
			"INSERT INTO donors (name, blood_type_id, phone, city, created_at) VALUES (?, ?, ?, ?, ?)",
			name, bloodTypeID, phone, city, time.Now().Format("2006-01-02"),
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
		bloodType := normalizeBloodType(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		hospital := strings.TrimSpace(r.FormValue("hospital"))
		if name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Recipient name and blood type are required.")
			return
		}
		bloodTypeID, err := getOrCreateBloodTypeID(db, bloodType)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not add recipient.")
			return
		}
		_, err = db.Exec(
			"INSERT INTO recipients (name, blood_type_id, phone, hospital, created_at) VALUES (?, ?, ?, ?, ?)",
			name, bloodTypeID, phone, hospital, time.Now().Format("2006-01-02"),
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
		units, _ := strconv.Atoi(r.FormValue("units"))
		expiry := strings.TrimSpace(r.FormValue("expiry_date"))
		if donorID == 0 || units <= 0 || expiry == "" {
			renderWithMessage(w, tmpl, db, "Donation requires donor, units, and expiry date.")
			return
		}
		bloodTypeID, err := getDonorBloodTypeID(db, donorID)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Donation requires a valid donor with blood type.")
			return
		}
		_, err = db.Exec(
			"INSERT INTO donations (donor_id, units, donation_date, expiry_date) VALUES (?, ?, ?, ?)",
			donorID, units, time.Now().Format("2006-01-02"), expiry,
		)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not add donation.")
			return
		}
		if err := upsertInventoryByTypeID(db, bloodTypeID, units); err != nil {
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
		var units int
		var bloodTypeID int
		err := db.QueryRow(`
			SELECT donors.blood_type_id, d.units
			FROM donations d
			JOIN donors ON donors.id = d.donor_id
			WHERE d.id = ? AND d.deleted_at IS NULL
		`, id).Scan(&bloodTypeID, &units)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Donation not found.")
			return
		}
		ok, err := consumeInventoryByTypeID(db, bloodTypeID, units)
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
		units, _ := strconv.Atoi(r.FormValue("units"))
		if recipientID == 0 || units <= 0 {
			renderWithMessage(w, tmpl, db, "Request requires recipient and units.")
			return
		}
		if _, err := getRecipientBloodTypeID(db, recipientID); err != nil {
			renderWithMessage(w, tmpl, db, "Request requires a valid recipient with blood type.")
			return
		}
		_, err := db.Exec(
			"INSERT INTO requests (recipient_id, units, status, request_date) VALUES (?, ?, ?, ?)",
			recipientID, units, "Pending", time.Now().Format("2006-01-02"),
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
		bloodType := normalizeBloodType(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		city := strings.TrimSpace(r.FormValue("city"))
		if id == 0 || name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Donor update requires id, name, and blood type.")
			return
		}
		bloodTypeID, err := getOrCreateBloodTypeID(db, bloodType)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not update donor.")
			return
		}
		_, err = db.Exec("UPDATE donors SET name = ?, blood_type_id = ?, phone = ?, city = ? WHERE id = ?", name, bloodTypeID, phone, city, id)
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
		bloodType := normalizeBloodType(r.FormValue("blood_type"))
		phone := strings.TrimSpace(r.FormValue("phone"))
		hospital := strings.TrimSpace(r.FormValue("hospital"))
		if id == 0 || name == "" || bloodType == "" {
			renderWithMessage(w, tmpl, db, "Recipient update requires id, name, and blood type.")
			return
		}
		bloodTypeID, err := getOrCreateBloodTypeID(db, bloodType)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Could not update recipient.")
			return
		}
		_, err = db.Exec("UPDATE recipients SET name = ?, blood_type_id = ?, phone = ?, hospital = ? WHERE id = ?", name, bloodTypeID, phone, hospital, id)
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
		var units int
		var bloodTypeID int
		err := db.QueryRow(`
			SELECT recipients.blood_type_id, r.units
			FROM requests r
			JOIN recipients ON recipients.id = r.recipient_id
			WHERE r.id = ?
		`, id).Scan(&bloodTypeID, &units)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Request not found.")
			return
		}
		ok, err := consumeInventoryByTypeID(db, bloodTypeID, units)
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
		units, _ := strconv.Atoi(r.FormValue("units"))
		status := strings.TrimSpace(r.FormValue("status"))
		if id == 0 || units <= 0 || status == "" {
			renderWithMessage(w, tmpl, db, "Request update requires id, units, and status.")
			return
		}

		var oldUnits int
		var oldStatus string
		err := db.QueryRow("SELECT units, status FROM requests WHERE id = ? AND deleted_at IS NULL", id).Scan(&oldUnits, &oldStatus)
		if err != nil {
			renderWithMessage(w, tmpl, db, "Request not found.")
			return
		}

		if oldStatus == "Fulfilled" {
			if status != "Fulfilled" || oldUnits != units {
				renderWithMessage(w, tmpl, db, "Cannot modify a fulfilled request.")
				return
			}
		}

		if oldStatus != "Fulfilled" && status == "Fulfilled" {
			bloodTypeID, err := getRequestBloodTypeID(db, id)
			if err != nil {
				renderWithMessage(w, tmpl, db, "Request is missing blood type.")
				return
			}
			ok, err := consumeInventoryByTypeID(db, bloodTypeID, units)
			if err != nil {
				renderWithMessage(w, tmpl, db, "Inventory update failed.")
				return
			}
			if !ok {
				renderWithMessage(w, tmpl, db, "Not enough inventory to fulfill request.")
				return
			}
		}

		_, err = db.Exec("UPDATE requests SET units = ?, status = ? WHERE id = ?", units, status, id)
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
	if err := migrateTo3NF(db); err != nil {
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
	if _, err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_inventory_blood_type_id ON inventory(blood_type_id)"); err != nil {
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

func normalizeBloodType(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func getOrCreateBloodTypeID(db *sql.DB, bloodType string) (int, error) {
	bloodType = normalizeBloodType(bloodType)
	if bloodType == "" {
		return 0, fmt.Errorf("blood type required")
	}
	var id int
	err := db.QueryRow("SELECT id FROM blood_types WHERE type = ?", bloodType).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := db.Exec("INSERT INTO blood_types (type) VALUES (?)", bloodType)
	if err != nil {
		return 0, err
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(lastID), nil
}

func getDonorBloodTypeID(db *sql.DB, donorID int) (int, error) {
	var id int
	err := db.QueryRow("SELECT blood_type_id FROM donors WHERE id = ? AND deleted_at IS NULL", donorID).Scan(&id)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("missing blood type")
	}
	return id, nil
}

func getRecipientBloodTypeID(db *sql.DB, recipientID int) (int, error) {
	var id int
	err := db.QueryRow("SELECT blood_type_id FROM recipients WHERE id = ? AND deleted_at IS NULL", recipientID).Scan(&id)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("missing blood type")
	}
	return id, nil
}

func getRequestBloodTypeID(db *sql.DB, requestID int) (int, error) {
	var id int
	err := db.QueryRow(`
		SELECT recipients.blood_type_id
		FROM requests r
		JOIN recipients ON recipients.id = r.recipient_id
		WHERE r.id = ? AND r.deleted_at IS NULL
	`, requestID).Scan(&id)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("missing blood type")
	}
	return id, nil
}

func tableHasColumn(db *sql.DB, table string, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
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
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func migrateTo3NF(db *sql.DB) error {
	hasBloodType, err := tableHasColumn(db, "donors", "blood_type")
	if err != nil {
		return err
	}
	if !hasBloodType {
		return nil
	}

	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS blood_types (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL UNIQUE
		)
	`); err != nil {
		return err
	}

	if _, err := db.Exec(`
		INSERT OR IGNORE INTO blood_types (type)
		SELECT DISTINCT UPPER(TRIM(blood_type)) FROM donors WHERE blood_type IS NOT NULL AND TRIM(blood_type) <> ''
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO blood_types (type)
		SELECT DISTINCT UPPER(TRIM(blood_type)) FROM recipients WHERE blood_type IS NOT NULL AND TRIM(blood_type) <> ''
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO blood_types (type)
		SELECT DISTINCT UPPER(TRIM(blood_type)) FROM inventory WHERE blood_type IS NOT NULL AND TRIM(blood_type) <> ''
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO blood_types (type) VALUES ('UNKNOWN')`); err != nil {
		return err
	}

	if _, err := db.Exec(`
		CREATE TABLE donors_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			blood_type_id INTEGER NOT NULL,
			phone TEXT,
			city TEXT,
			created_at TEXT NOT NULL,
			deleted_at TEXT,
			FOREIGN KEY(blood_type_id) REFERENCES blood_types(id)
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE recipients_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			blood_type_id INTEGER NOT NULL,
			phone TEXT,
			hospital TEXT,
			created_at TEXT NOT NULL,
			deleted_at TEXT,
			FOREIGN KEY(blood_type_id) REFERENCES blood_types(id)
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE donations_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			donor_id INTEGER NOT NULL,
			units INTEGER NOT NULL,
			donation_date TEXT NOT NULL,
			expiry_date TEXT NOT NULL,
			deleted_at TEXT,
			FOREIGN KEY(donor_id) REFERENCES donors(id)
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE requests_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			recipient_id INTEGER NOT NULL,
			units INTEGER NOT NULL,
			status TEXT NOT NULL,
			request_date TEXT NOT NULL,
			deleted_at TEXT,
			FOREIGN KEY(recipient_id) REFERENCES recipients(id)
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE inventory_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			blood_type_id INTEGER NOT NULL UNIQUE,
			units INTEGER NOT NULL,
			deleted_at TEXT,
			FOREIGN KEY(blood_type_id) REFERENCES blood_types(id)
		)
	`); err != nil {
		return err
	}

	if _, err := db.Exec(`
		INSERT INTO donors_new (id, name, blood_type_id, phone, city, created_at, deleted_at)
		SELECT id, name,
			COALESCE((SELECT id FROM blood_types WHERE type = UPPER(TRIM(blood_type))),
				(SELECT id FROM blood_types WHERE type = 'UNKNOWN')),
			phone, city, created_at, deleted_at
		FROM donors
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT INTO recipients_new (id, name, blood_type_id, phone, hospital, created_at, deleted_at)
		SELECT id, name,
			COALESCE((SELECT id FROM blood_types WHERE type = UPPER(TRIM(blood_type))),
				(SELECT id FROM blood_types WHERE type = 'UNKNOWN')),
			phone, hospital, created_at, deleted_at
		FROM recipients
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT INTO donations_new (id, donor_id, units, donation_date, expiry_date, deleted_at)
		SELECT id, donor_id, units, donation_date, expiry_date, deleted_at
		FROM donations
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT INTO requests_new (id, recipient_id, units, status, request_date, deleted_at)
		SELECT id, recipient_id, units, status, request_date, deleted_at
		FROM requests
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT INTO inventory_new (id, blood_type_id, units, deleted_at)
		SELECT id,
			COALESCE((SELECT id FROM blood_types WHERE type = UPPER(TRIM(blood_type))),
				(SELECT id FROM blood_types WHERE type = 'UNKNOWN')),
			units, deleted_at
		FROM inventory
	`); err != nil {
		return err
	}

	if _, err := db.Exec("DROP TABLE donors"); err != nil {
		return err
	}
	if _, err := db.Exec("DROP TABLE recipients"); err != nil {
		return err
	}
	if _, err := db.Exec("DROP TABLE donations"); err != nil {
		return err
	}
	if _, err := db.Exec("DROP TABLE requests"); err != nil {
		return err
	}
	if _, err := db.Exec("DROP TABLE inventory"); err != nil {
		return err
	}

	if _, err := db.Exec("ALTER TABLE donors_new RENAME TO donors"); err != nil {
		return err
	}
	if _, err := db.Exec("ALTER TABLE recipients_new RENAME TO recipients"); err != nil {
		return err
	}
	if _, err := db.Exec("ALTER TABLE donations_new RENAME TO donations"); err != nil {
		return err
	}
	if _, err := db.Exec("ALTER TABLE requests_new RENAME TO requests"); err != nil {
		return err
	}
	if _, err := db.Exec("ALTER TABLE inventory_new RENAME TO inventory"); err != nil {
		return err
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	return nil
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
	rows, err := db.Query(`
		SELECT d.id, d.name, bt.type, d.phone, d.city, d.created_at
		FROM donors d
		JOIN blood_types bt ON bt.id = d.blood_type_id
		WHERE d.deleted_at IS NULL
		ORDER BY d.id DESC
	`)
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
	rows, err := db.Query(`
		SELECT r.id, r.name, bt.type, r.phone, r.hospital, r.created_at
		FROM recipients r
		JOIN blood_types bt ON bt.id = r.blood_type_id
		WHERE r.deleted_at IS NULL
		ORDER BY r.id DESC
	`)
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
		SELECT d.id, d.donor_id, donors.name, bt.type, d.units, d.donation_date, d.expiry_date
		FROM donations d
		JOIN donors ON donors.id = d.donor_id
		JOIN blood_types bt ON bt.id = donors.blood_type_id
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
	rows, err := db.Query(`
		SELECT bt.type, i.units
		FROM inventory i
		JOIN blood_types bt ON bt.id = i.blood_type_id
		WHERE i.deleted_at IS NULL
		ORDER BY bt.type
	`)
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
		SELECT r.id, r.recipient_id, recipients.name, bt.type, r.units, r.status, r.request_date
		FROM requests r
		JOIN recipients ON recipients.id = r.recipient_id
		JOIN blood_types bt ON bt.id = recipients.blood_type_id
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

func upsertInventoryByTypeID(db *sql.DB, bloodTypeID int, units int) error {
	res, err := db.Exec("UPDATE inventory SET units = units + ?, deleted_at = NULL WHERE blood_type_id = ?", units, bloodTypeID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		_, err = db.Exec("INSERT INTO inventory (blood_type_id, units, deleted_at) VALUES (?, ?, NULL)", bloodTypeID, units)
		return err
	}
	return nil
}

func consumeInventoryByTypeID(db *sql.DB, bloodTypeID int, units int) (bool, error) {
	var current int
	err := db.QueryRow("SELECT units FROM inventory WHERE blood_type_id = ? AND deleted_at IS NULL", bloodTypeID).Scan(&current)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current < units {
		return false, nil
	}
	_, err = db.Exec("UPDATE inventory SET units = units - ? WHERE blood_type_id = ?", units, bloodTypeID)
	if err != nil {
		return false, err
	}
	return true, nil
}
