
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"filetransfer/internal/server"
)

const uploadsDir = "uploads"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Использование: %s <порт>\n", os.Args[0])
		os.Exit(2)
	}
	port := os.Args[1]
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		fmt.Fprintln(os.Stderr, "Ошибка: порт должен быть числом от 1 до 65535")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		stop() // повторный Ctrl+C завершит процесс сразу
		log.Println("останавливаюсь: жду завершения активных передач (Ctrl+C ещё раз — выход сразу)")
	}()

	if err := server.New(port, uploadsDir).Run(ctx); err != nil {
		log.Fatalf("ошибка: %v", err)
	}
	log.Println("сервер остановлен")
}
