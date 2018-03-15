.PHONY: test
test: elections.cgi
	go test .

elections.cgi: main.go
	go build -v -o elections.cgi .

index.html: elections.cgi templates/index.html
	./elections.cgi -index

deploy: test elections.cgi index.html
	rsync -ravzh --progress . q7w9a@remote.ugrad.cs.ubc.ca:~/public_html/elections
