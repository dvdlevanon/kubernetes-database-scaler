
build:
	mkdir -p build && go build -o build/kubernetes-database-scaler

docker:
	docker build -t kubernetes-database-scaler .

clean:
	rm -rf build
