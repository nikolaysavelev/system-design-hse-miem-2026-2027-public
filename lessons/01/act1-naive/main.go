// Сервис продажи билетов на концерт. Бронирование мест и оплата.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

var db *sql.DB

type Booking struct {
	ID        int    `json:"id"`
	SeatID    int    `json:"seat_id"`
	UserID    string `json:"user_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/tickets?sslmode=disable"
	}
	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/seats", listSeats)
	http.HandleFunc("/book", bookSeat)
	http.HandleFunc("/pay", paySeat)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// GET /seats?event_id=1
func listSeats(w http.ResponseWriter, r *http.Request) {
	eventID := r.URL.Query().Get("event_id")
	rows, err := db.Query("SELECT * FROM seats WHERE event_id = $1", eventID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var seats []map[string]interface{}
	for rows.Next() {
		var id, ev, row, seat int
		rows.Scan(&id, &ev, &row, &seat)
		var status string
		db.QueryRow("SELECT status FROM bookings WHERE seat_id = $1", id).Scan(&status)
		if status == "" {
			status = "free"
		}
		seats = append(seats, map[string]interface{}{"id": id, "row": row, "seat": seat, "status": status})
	}
	json.NewEncoder(w).Encode(seats)
}

// POST /book?seat_id=1&user_id=alice
func bookSeat(w http.ResponseWriter, r *http.Request) {
	seatID := r.URL.Query().Get("seat_id")
	userID := r.URL.Query().Get("user_id")

	// проверяем, что место свободно
	rows, err := db.Query("SELECT * FROM bookings WHERE seat_id = $1", seatID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if rows.Next() {
		rows.Close()
		http.Error(w, "seat already booked", 409)
		return
	}
	rows.Close()

	var id int
	err = db.QueryRow("INSERT INTO bookings (seat_id, user_id, status) VALUES ($1, $2, 'reserved') RETURNING id", seatID, userID).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintf(w, `{"booking_id": %d, "status": "reserved"}`, id)
}

// POST /pay?booking_id=1&amount=5000
func paySeat(w http.ResponseWriter, r *http.Request) {
	bookingID := r.URL.Query().Get("booking_id")
	amount := r.URL.Query().Get("amount")

	// имитация оплаты
	_, err := db.Exec("INSERT INTO payments (booking_id, amount, status) VALUES ($1, $2, 'ok')", bookingID, amount)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_, err = db.Exec("UPDATE bookings SET status = 'paid' WHERE id = $1", bookingID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintf(w, `{"booking_id": %s, "status": "paid"}`, bookingID)
}
