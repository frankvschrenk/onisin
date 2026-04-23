.PHONY: run form table gallery person build tidy

# --- Build ---
# Erzeugt eine native Binary für das aktuelle System.
# Fyne benötigt CGo — auf macOS ist das automatisch verfügbar.
# Das Binary lädt DSL und JSON zur Laufzeit via Flags:
#   ./oos-dsl -dsl path/to/screen.xml -data path/to/data.json
build:
	go build -o oos-dsl .

# --- Run targets ---
table:
	go run main.go -dsl testdata/user-list.xml -data testdata/user-list.json

person:
	go run main.go -dsl testdata/person-detail.xml -data testdata/person-detail.json
