.PHONY: all build backend frontend run dev-backend dev-frontend tidy test clean

all: build

## build: 프론트엔드 + 백엔드 전체 빌드
build: frontend backend

## frontend: React 프론트엔드 설치 및 빌드 -> web/dist
frontend:
	cd web && npm install && npm run build

## backend: Go 서버 바이너리 빌드 -> bin/dvc-cvp
backend:
	go build -o bin/dvc-cvp ./cmd/server

## run: 빌드된 서버 실행 (config/config.yaml 사용, 없으면 demo 모드)
run: backend
	./bin/dvc-cvp -config config/config.yaml

## dev-backend: 데모 모드로 백엔드 실행 (:8080)
dev-backend:
	go run ./cmd/server

## dev-frontend: Vite 개발 서버 (:5173, /api 는 :8080 으로 프록시)
dev-frontend:
	cd web && npm run dev

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -rf bin web/dist
