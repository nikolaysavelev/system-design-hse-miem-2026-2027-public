// КРАСНЫЙ ПРИМЕР (tools/check-sql-owners.py): модуль catalog читает таблицу holds (владелец booking) и
// запрос без префикса /* module.name */.
package red

const badOwner = "/* catalog.seat_states */ SELECT seat_id, status FROM holds WHERE event_id = $1"
const noPrefix = "SELECT id FROM events WHERE id = $1"
