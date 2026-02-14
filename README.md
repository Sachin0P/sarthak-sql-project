# Blood Bank Management DBMS

Simple DBMS project using Go + SQLite with a clean HTML frontend.

## Run

```bash
go mod tidy
go run .
```

Open `http://localhost:8080` in your browser.

## Notes

- SQLite database file: `bloodbank.db`
- Backend: Go + `modernc.org/sqlite` (pure Go driver)
- Frontend: HTML templates + CSS
