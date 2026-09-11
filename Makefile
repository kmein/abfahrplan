all: abfahrplan GTFS.zip timetable.pdf

GTFS.zip:
	wget https://unternehmen.vbb.de/fileadmin/user_upload/VBB/Dokumente/API-Datensaetze/gtfs-mastscharf/GTFS.zip -O $@

abfahrplan: $(wildcard *.go cmd/abfahrplan/*.go timetable/*.go)
	go build ./cmd/abfahrplan

timetable.pdf: render/timetable.typ timetable.json
	typst compile --root . --input data=/timetable.json $< $@

timetable.json: abfahrplan GTFS.zip
	./abfahrplan -s "Albrechtstr" # -r 140 -r M46
