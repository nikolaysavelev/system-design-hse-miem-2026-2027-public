package domain

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// sector: rows рядов по perRow мест, id = (row-1)*perRow + no.
func sector(rows, perRow int) []SeatPos {
	var out []SeatPos
	for r := 1; r <= rows; r++ {
		for n := 1; n <= perRow; n++ {
			out = append(out, SeatPos{ID: int64((r-1)*perRow + n), RowNo: r, No: n})
		}
	}
	return out
}

func taken(ids ...int64) map[int64]bool {
	m := map[int64]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func TestFindAdjacent_FirstFreeSegment(t *testing.T) {
	s := sector(3, 10)
	if got := FindAdjacent(s, nil, 3, 0); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("пустой сектор: %v", got)
	}
	// в первом ряду заняты 3 и 6: первый свободный отрезок из трех — 7..9
	if got := FindAdjacent(s, taken(3, 6), 3, 0); !reflect.DeepEqual(got, []int64{7, 8, 9}) {
		t.Fatalf("с занятыми местами: %v", got)
	}
	// отрезок из двух помещается в 1..2
	if got := FindAdjacent(s, taken(3, 6), 2, 0); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("n=2: %v", got)
	}
}

func TestFindAdjacent_NextRowWhenRowFull(t *testing.T) {
	s := sector(2, 4)
	// ряд 1: занято место 2, отрезка из трех нет — берем ряд 2
	if got := FindAdjacent(s, taken(2), 3, 1); !reflect.DeepEqual(got, []int64{5, 6, 7}) {
		t.Fatalf("переход на следующий ряд: %v", got)
	}
}

func TestFindAdjacent_PreferRowThenWrap(t *testing.T) {
	s := sector(3, 4)
	if got := FindAdjacent(s, nil, 2, 2); !reflect.DeepEqual(got, []int64{5, 6}) {
		t.Fatalf("prefer_row=2: %v", got)
	}
	// ряды 2 и 3 заняты целиком — возвращаемся к ряду 1
	if got := FindAdjacent(s, taken(5, 6, 7, 8, 9, 10, 11, 12), 2, 2); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("переход к рядам до prefer_row: %v", got)
	}
}

func TestFindAdjacent_NotFound(t *testing.T) {
	s := sector(2, 4)
	// свободны только места через одно — смежной пары нет
	if got := FindAdjacent(s, taken(2, 4, 6, 8), 2, 0); got != nil {
		t.Fatalf("ожидалось nil, получено %v", got)
	}
	if got := FindAdjacent(s, nil, 5, 0); got != nil {
		t.Fatalf("отрезок длиннее ряда: %v", got)
	}
	standing := []SeatPos{{ID: 1, RowNo: 0, No: 1}, {ID: 2, RowNo: 0, No: 2}}
	if got := FindAdjacent(standing, nil, 2, 0); got != nil {
		t.Fatalf("танцпол без рядов: %v", got)
	}
}

func TestFindAdjacent_GapInNumbering(t *testing.T) {
	// места 1, 2, 4, 5 (места 3 в ряду нет, например проход) — 2 и 4 не смежны
	s := []SeatPos{{ID: 1, RowNo: 1, No: 1}, {ID: 2, RowNo: 1, No: 2}, {ID: 4, RowNo: 1, No: 4}, {ID: 5, RowNo: 1, No: 5}}
	if got := FindAdjacent(s, taken(1), 2, 0); !reflect.DeepEqual(got, []int64{4, 5}) {
		t.Fatalf("разрыв нумерации: %v", got)
	}
}

func TestGroupSizeAndLimit(t *testing.T) {
	for n, ok := range map[int]bool{1: false, 2: true, 4: true, 5: false} {
		if (ValidateGroupSize(n) == nil) != ok {
			t.Errorf("ValidateGroupSize(%d)", n)
		}
	}
	if !CanHoldGroup(0, 4) || CanHoldGroup(1, 4) || !CanHoldGroup(2, 2) {
		t.Fatal("лимит 4 должен учитывать всю группу")
	}
}

func TestNewGroupHolds(t *testing.T) {
	g := Group{ID: uuid.New(), EventID: uuid.New(), UserID: uuid.New(), N: 2}
	now := time.Now()
	hs := NewGroupHolds(g, []int64{7, 8}, now, time.Minute)
	if len(hs) != 2 || *hs[0].GroupID != g.ID || *hs[1].GroupID != g.ID || hs[0].ExpiresAt != hs[1].ExpiresAt || hs[1].SeatID != 8 {
		t.Fatalf("неверные hold'ы группы: %+v", hs)
	}
}
