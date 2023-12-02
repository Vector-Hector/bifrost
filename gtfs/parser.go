package gtfs_stream

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"github.com/artonge/go-gtfs"
	"os"
	"reflect"
	"strconv"
	"strings"
)

func CountRows(fileName string) (int, error) {
	f, err := os.Open(fileName)
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

func IterateStops(fileName string, handler func(int, *gtfs.Stop) bool) error {
	return iterateCsvFile(fileName, gtfs.Stop{}, func(index int, out *gtfs.Stop) bool {
		return handler(index, out)
	})
}

func IterateServices(fileName string, handler func(int, *gtfs.Calendar) bool) error {
	return iterateCsvFile(fileName, gtfs.Calendar{}, func(index int, out *gtfs.Calendar) bool {
		return handler(index, out)
	})
}

func IterateCalendarDates(fileName string, handler func(int, *gtfs.CalendarDate) bool) error {
	return iterateCsvFile(fileName, gtfs.CalendarDate{}, func(index int, out *gtfs.CalendarDate) bool {
		return handler(index, out)
	})
}

func IterateRoutes(fileName string, handler func(int, *gtfs.Route) bool) error {
	return iterateCsvFile(fileName, gtfs.Route{}, func(index int, out *gtfs.Route) bool {
		return handler(index, out)
	})
}

func IterateTrips(fileName string, handler func(int, *gtfs.Trip) bool) error {
	return iterateCsvFile(fileName, gtfs.Trip{}, func(index int, out *gtfs.Trip) bool {
		return handler(index, out)
	})
}

func IterateStopTimes(fileName string, handler func(int, *gtfs.StopTime) bool) error {
	return iterateCsvFile(fileName, gtfs.StopTime{}, func(index int, out *gtfs.StopTime) bool {
		return handler(index, out)
	})
}

func iterateCsvFile[T any](fileName string, outInstance T, handler func(int, *T) bool) error {
	f, err := os.Open(fileName)
	if err != nil {
		return err
	}

	defer f.Close()

	r := csv.NewReader(f)

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
