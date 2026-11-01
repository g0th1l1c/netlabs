// Package ui — консольный интерфейс. Только ввод и вывод, никакой логики:
// всё, что связано с данными, делает пакет service.
package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"places/internal/model"
	"places/internal/service"
)

// Console — интерактивное консольное приложение.
type Console struct {
	svc *service.Service
	in  *bufio.Reader
	out io.Writer
}

// NewConsole создаёт интерфейс, читающий из in и пишущий в out.
func NewConsole(svc *service.Service, in io.Reader, out io.Writer) *Console {
	return &Console{svc: svc, in: bufio.NewReader(in), out: out}
}

// Run крутит основной цикл: поиск -> выбор -> отчёт -> снова поиск.
// Завершается, когда пользователь ввёл "q", закрыт ввод или отменён ctx.
func (c *Console) Run(ctx context.Context) {
	fmt.Fprintln(c.out, "Поиск интересных мест. Для выхода введите q.")

	for ctx.Err() == nil {
		query, ok := c.ask("\nВведите название места: ")
		if !ok || strings.EqualFold(query, "q") {
			return
		}
		if query == "" {
			continue
		}

		// Пункт 2: ищем локации. Блокируемся только на ожидании
		// конечного результата этого шага — это допустимо для консоли.
		fmt.Fprintln(c.out, "Ищу...")
		found := <-c.svc.FindLocations(ctx, query)
		if found.Err != nil {
			fmt.Fprintf(c.out, "Ошибка поиска: %v\n", found.Err)
			continue
		}
		if len(found.Locations) == 0 {
			fmt.Fprintln(c.out, "Ничего не найдено. Попробуйте другой запрос.")
			continue
		}

		loc, ok := c.choose(found.Locations)
		if !ok {
			continue
		}

		// Пункты 4–5: погода, места и описания одним общим отчётом.
		fmt.Fprintf(c.out, "\nЗагружаю данные для «%s»...\n", loc.Title())
		report := <-c.svc.LoadReport(ctx, loc)
		if ctx.Err() != nil {
			return
		}
		c.printReport(report)
	}
}

// ask выводит приглашение и читает строку. ok=false, если ввод закончился.
func (c *Console) ask(prompt string) (string, bool) {
	fmt.Fprint(c.out, prompt)
	line, err := c.in.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimSpace(line), true
}

// choose показывает список локаций и просит выбрать одну (пункт 3).
// Пустой ввод или 0 — вернуться к поиску.
func (c *Console) choose(locs []model.Location) (model.Location, bool) {
	fmt.Fprintln(c.out, "\nНайдено:")
	for i, l := range locs {
		fmt.Fprintf(c.out, "  %d. %s\n", i+1, l.Title())
	}

	for {
		answer, ok := c.ask("Выберите номер (0 — новый поиск): ")
		if !ok || answer == "" || answer == "0" {
			return model.Location{}, false
		}
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(locs) {
			fmt.Fprintf(c.out, "Введите число от 1 до %d.\n", len(locs))
			continue
		}
		return locs[n-1], true
	}
}

// printReport выводит итоговый отчёт целиком (пункт 6).
func (c *Console) printReport(r model.Report) {
	line := strings.Repeat("=", 60)
	fmt.Fprintf(c.out, "\n%s\n%s\n%s\n", line, r.Location.Title(), line)

	fmt.Fprintln(c.out, "\nПОГОДА")
	switch {
	case r.Weather != nil:
		w := r.Weather
		fmt.Fprintf(c.out, "  %s, %.1f °C (ощущается как %.1f °C)\n", capitalize(w.Description), w.TempC, w.FeelsLikeC)
		fmt.Fprintf(c.out, "  Влажность %.0f%%, давление %.0f мм рт. ст., ветер %.1f м/с\n", w.Humidity, w.PressureHPa*0.750062, w.WindMS)
	default:
		fmt.Fprintf(c.out, "  Не удалось получить погоду: %v\n", r.WeatherErr)
	}

	fmt.Fprintln(c.out, "\nИНТЕРЕСНЫЕ МЕСТА")
	switch {
	case r.PlacesErr != nil:
		fmt.Fprintf(c.out, "  Не удалось получить список мест: %v\n", r.PlacesErr)
	case len(r.Places) == 0:
		fmt.Fprintln(c.out, "  Поблизости ничего не найдено.")
	}
	for i, p := range r.Places {
		fmt.Fprintf(c.out, "\n  %d. %s (%.0f м)\n", i+1, p.Name, p.Distance)
		if p.Kinds != "" {
			fmt.Fprintf(c.out, "     Категории: %s\n", strings.ReplaceAll(strings.ReplaceAll(p.Kinds, "_", " "), ",", ", "))
		}
		if p.Address != "" {
			fmt.Fprintf(c.out, "     Адрес: %s\n", p.Address)
		}
		switch {
		case p.DetailsErr != nil:
			fmt.Fprintf(c.out, "     Описание недоступно: %v\n", p.DetailsErr)
		case p.Description == "":
			fmt.Fprintln(c.out, "     Описания нет.")
		default:
			fmt.Fprintf(c.out, "     %s\n", shorten(p.Description, 300))
		}
		if p.URL != "" {
			fmt.Fprintf(c.out, "     Ссылка: %s\n", p.URL)
		}
	}
	fmt.Fprintln(c.out)
}

// shorten обрезает текст до max символов (именно символов, а не байтов:
// русские буквы занимают по 2 байта).
func shorten(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ") // убрать переносы строк и лишние пробелы
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// capitalize делает первую букву заглавной.
func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
