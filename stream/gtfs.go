package stream

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"github.com/artonge/go-gtfs"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
)

type GTFSFile struct {
	Reader *zip.ReadCloser
}

func (g *GTFSFile) Close() error {
	return g.Reader.Close()
}

func OpenGTFS(path string) (*GTFSFile, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}

	return &GTFSFile{
		Reader: reader,
	}, nil
}

func (g *GTFSFile) CountRows(fileName string) (int, error) {
	f, err := g.Reader.Open(fileName)
	if err != nil {
		return 0, err
	}

	defer f.Close()

	scanner := bufio.NewScanner(f)

	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		count++
	}

	return count - 1, nil // subtract 1 for the header
}

func (g *GTFSFile) IterateStops(handler func(int, *gtfs.Stop) bool) error {
	return iterateCsvFile(g, "stops.txt", ',', gtfs.Stop{}, func(index int, out *gtfs.Stop) bool {
		return handler(index, out)
	})
}

func (g *GTFSFile) IterateServices(handler func(int, *gtfs.Calendar) bool) error {
	return iterateCsvFile(g, "calendar.txt", ',', gtfs.Calendar{}, func(index int, out *gtfs.Calendar) bool {
		return handler(index, out)
	})
}

func (g *GTFSFile) IterateCalendarDates(handler func(int, *gtfs.CalendarDate) bool) error {
	return iterateCsvFile(g, "calendar_dates.txt", ',', gtfs.CalendarDate{}, func(index int, out *gtfs.CalendarDate) bool {
		return handler(index, out)
	})
}

func (g *GTFSFile) IterateRoutes(handler func(int, *gtfs.Route) bool) error {
	return iterateCsvFile(g, "routes.txt", ',', gtfs.Route{}, func(index int, out *gtfs.Route) bool {
		return handler(index, out)
	})
}

func (g *GTFSFile) IterateTrips(handler func(int, *gtfs.Trip) bool) error {
	return iterateCsvFile(g, "trips.txt", ',', gtfs.Trip{}, func(index int, out *gtfs.Trip) bool {
		return handler(index, out)
	})
}

func (g *GTFSFile) IterateStopTimes(handler func(int, *gtfs.StopTime) bool) error {
	return iterateCsvFile(g, "stop_times.txt", ',', gtfs.StopTime{}, func(index int, out *gtfs.StopTime) bool {
		return handler(index, out)
	})
}

func iterateCsvFile[T any](g *GTFSFile, fileName string, comma rune, outInstance T, handler func(int, *T) bool) error {
	f, err := g.Reader.Open(fileName)
	if err != nil {
		return err
	}

	defer f.Close()

	return iterateCsvReader(f, comma, outInstance, handler)
}

func iterateCsvReader[T any](f io.Reader, comma rune, outInstance T, handler func(int, *T) bool) error {
	f = skipBOM(f)

	r := csv.NewReader(f)
	r.Comma = comma

	header, err := r.Read()
	if err != nil {
		return err
	}

	headerMap := make(map[string]int)
	for i, v := range header {
		headerMap[v] = i
	}

	typ := reflect.TypeOf(outInstance)
	fmt.Println(typ.String())

	currentStruct := reflect.New(typ).Elem()
	zeroValue := reflect.Zero(typ)
	pos := 0

	for {
		line, err := r.Read()
		if err != nil {
			break
		}

		err = readLine(line, headerMap, currentStruct)
		if err != nil {
			return err
		}

		t := currentStruct.Interface().(T)

		if !handler(pos, &t) {
			break
		}

		currentStruct.Set(zeroValue)
		pos++
	}

	return nil
}

func iterateCsvFileStringBuffer(fileName string, comma rune, wantedOrder []string, threadCount int, handler func(StringArr)) error {
	f, err := os.Open(fileName)
	if err != nil {
		return err
	}

	defer f.Close()

	return iterateCsvReaderStringBuffer(f, comma, wantedOrder, threadCount, handler)
}
func iterateCsvReaderStringBuffer(f io.Reader, comma rune, wantedOrder []string, threadCount int, handler func(StringArr)) error {
	f = skipBOM(f)

	r := csv.NewReader(f)
	r.Comma = comma

	header, err := r.Read()
	if err != nil {
		return err
	}

	headerMap := make(map[string]int)
	for i, v := range header {
		headerMap[v] = i
	}

	reorder := make([]int, len(wantedOrder))
	for i, v := range wantedOrder {
		reorder[i] = headerMap[v]
	}

	// create one reader goroutine and multiple handler goroutines
	// the reader goroutine reads lines and sends them to the handler goroutines using a channel
	// the handler goroutines parse the lines and send them to the handler function

	done := make(chan bool)
	lines := make(chan []string, 500)

	go func() {
		for {
			line, err := r.Read()
			if err != nil {
				break
			}
			lines <- line
		}
		close(lines)
	}()

	for i := 0; i < threadCount; i++ {
		go func() {
			currentLine := make(StringArr, len(wantedOrder))

			for line := range lines {
				for j, v := range reorder {
					currentLine[j] = line[v]
				}

				handler(currentLine)
			}

			done <- true
		}()
	}

	for i := 0; i < threadCount; i++ {
		<-done
	}

	return nil
}

// Skip the Byte Order Mark (BOM) if it exists.
// @param file: the io.Reader to read from.
func skipBOM(file io.Reader) io.Reader {
	// Read the first 3 bytes.
	bom := make([]byte, 3)
	_, err := file.Read(bom)
	if err != nil {
		return file
	}

	// If the first 3 bytes are not the BOM, reset the reader.
	if bom[0] != 0xEF || bom[1] != 0xBB || bom[2] != 0xBF {
		return io.MultiReader(bytes.NewReader(bom), file)
	}

	return file
}

func readLine(line []string, headerMap map[string]int, out reflect.Value) error {
	for j := 0; j < out.NumField(); j++ {
		propertyTag := out.Type().Field(j).Tag.Get("csv")
		if propertyTag == "" {
			continue
		}

		propertyPosition, ok := headerMap[propertyTag]
		if !ok {
			continue
		}

		err := storeValue(line[propertyPosition], out.Field(j))
		if err != nil {
			return fmt.Errorf("line: %v to slice: %v:\n	==> %v", line, out, err)
		}
	}

	return nil
}

// Set the value of the valRv to rawValue.
// @param rawValue: the value, as a string, that we want to store.
// @param valRv: the reflected value where we want to store our value.
// @return an error if one occurs.
func storeValue(rawValue string, valRv reflect.Value) error {
	rawValue = strings.TrimSpace(rawValue)
	switch valRv.Kind() {
	case reflect.String:
		valRv.SetString(rawValue)
	case reflect.Uint32:
		fallthrough
	case reflect.Uint64:
		fallthrough
	case reflect.Uint:
		value, err := strconv.ParseUint(rawValue, 10, 64)
		if err != nil && rawValue != "" {
			return fmt.Errorf("error parsing uint '%v':\n	==> %v", rawValue, err)
		}
		valRv.SetUint(value)
	case reflect.Int32:
		fallthrough
	case reflect.Int64:
		fallthrough
	case reflect.Int:
		value, err := strconv.ParseInt(rawValue, 10, 64)
		if err != nil && rawValue != "" {
			return fmt.Errorf("error parsing int '%v':\n	==> %v", rawValue, err)
		}
		valRv.SetInt(value)
	case reflect.Float32:
		fallthrough
	case reflect.Float64:
		value, err := strconv.ParseFloat(rawValue, 64)
		if err != nil && rawValue != "" {
			return fmt.Errorf("error parsing float '%v':\n	==> %v", rawValue, err)
		}
		valRv.SetFloat(value)
	case reflect.Bool:
		value, err := strconv.ParseBool(rawValue)
		if err != nil && rawValue != "" {
			return fmt.Errorf("error parsing bool '%v':\n	==> %v", rawValue, err)
		}
		valRv.SetBool(value)
	}

	return nil
}
