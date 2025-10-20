all: abfahrplan GTFS.zip timetable.pdf

GTFS.zip:
	wget https://unternehmen.vbb.de/fileadmin/user_upload/VBB/Dokumente/API-Datensaetze/gtfs-mastscharf/GTFS.zip -O $@

abfahrplan: main.go
	go build

timetable.pdf: timetable.typ timetable.json
	typst compile $<

timetable.json: abfahrplan GTFS.zip
	./abfahrplan -s "Albrechtstr."
