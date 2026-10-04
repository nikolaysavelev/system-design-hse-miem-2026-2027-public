package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAdmission_ValidFor(t *testing.T) {
	now := time.Now()
	ev, user := uuid.New(), uuid.New()
	a := NewAdmission(ev, user, now, 10*time.Minute)
	if len(a.Token) != 32 {
		t.Fatalf("ожидался токен из 32 hex-символов, получено %q", a.Token)
	}
	if !a.ValidFor(ev, user, now.Add(time.Minute)) {
		t.Fatal("токен должен действовать в пределах TTL")
	}
	if a.ValidFor(ev, user, now.Add(11*time.Minute)) {
		t.Fatal("токен не должен действовать после TTL")
	}
	if a.ValidFor(uuid.New(), user, now) || a.ValidFor(ev, uuid.New(), now) {
		t.Fatal("токен не должен подходить другому мероприятию или пользователю")
	}
}
