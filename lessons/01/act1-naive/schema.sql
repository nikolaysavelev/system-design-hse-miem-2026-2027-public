CREATE TABLE events (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE seats (
    id SERIAL PRIMARY KEY,
    event_id INT REFERENCES events(id),
    row_no INT,
    seat_no INT
);

CREATE TABLE bookings (
    id SERIAL PRIMARY KEY,
    seat_id INT REFERENCES seats(id),
    user_id TEXT,
    status TEXT,          -- reserved | paid
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE payments (
    id SERIAL PRIMARY KEY,
    booking_id INT REFERENCES bookings(id),
    amount NUMERIC,
    status TEXT
);
