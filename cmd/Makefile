containerbin:
	GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -o atompubpg
	docker build -t xtracdev/atompubpg:latest .

clean:
	rm -f atompubpg
