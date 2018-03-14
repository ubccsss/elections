.PHONY: test
test: elections.cgi
	go test -v .

elections.cgi: main.go
	go build -v -o elections.cgi .

deploy: test elections.cgi
	rsync -ravzh --progress . q7w9a@remote.ugrad.cs.ubc.ca:~/public_html/elections
