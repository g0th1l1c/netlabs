package main

import (
	"fmt"
	"os"
	"strconv"

	"filetransfer/internal/client"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Использование: %s <путь к файлу> <хост> <порт>\n", os.Args[0])
		os.Exit(2)
	}
	filePath, host, port := os.Args[1], os.Args[2], os.Args[3]

	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		fmt.Fprintln(os.Stderr, "Ошибка: порт должен быть числом от 1 до 65535")
		os.Exit(2)
	}

	if err := client.Send(filePath, host, port); err != nil {
		fmt.Fprintf(os.Stderr, "Передача НЕ удалась: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Передача завершена успешно: сервер подтвердил получение файла.")
}
