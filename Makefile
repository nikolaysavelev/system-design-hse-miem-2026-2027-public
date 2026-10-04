# Корневой Makefile курса. Делегирует в soldout/Makefile; здесь — сетка занятий и демо.
SOLDOUT := $(MAKE) -C soldout

.PHONY: help up up-01 up-lite down seed smoke storm hot-row invariants lint-arch test build doctor \
        demo-01-act1 demo-01-act2 demo-01-act3

help:
	@echo "Корневые цели:"
	@echo "  up / up-01       поднять стенд занятия 1        down     остановить и удалить volume'ы"
	@echo "  up-lite          lite-профиль (compose/lite.yml)"
	@echo "  smoke|storm|hot-row  k6-сценарии               invariants  инвариант-чекер"
	@echo "  lint-arch | test | build                        seed     повторно применить seed"
	@echo "  demo-01-act1..3  подсказки по актам занятия 1   doctor   проверка окружения"

doctor: ; tools/doctor.sh
up up-01: ; $(SOLDOUT) up
up-lite:  ; $(SOLDOUT) up-lite
down:     ; $(SOLDOUT) down
seed:     ; $(SOLDOUT) seed
smoke:    ; $(SOLDOUT) smoke
storm:    ; $(SOLDOUT) storm
hot-row:  ; $(SOLDOUT) hot-row
invariants: ; $(SOLDOUT) invariants
lint-arch: ; $(SOLDOUT) lint-arch
test:     ; $(SOLDOUT) test
build:    ; $(SOLDOUT) build

demo-01-act1:
	@echo "Акт 1: git switch demo/act1-naive && cat lessons/01/act1-review.md"
demo-01-act2:
	@echo "Акт 2: git switch demo/act2-start && cd soldout && make down && make up  — далее промпт из lessons/01/demo.md"
demo-01-act3:
	@echo "Акт 3: git switch demo/act3-violation && make lint-arch  (ожидается красный)"
