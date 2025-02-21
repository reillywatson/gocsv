package gocsv

import (
	"fmt"
	"io"
	"reflect"
)

type encoder struct {
	out io.Writer
}

func newEncoder(out io.Writer) *encoder {
	return &encoder{out}
}

func writeFromChan(writer *SafeCSVWriter, c <-chan interface{}) error {
	// Get the first value. It wil determine the header structure.
	firstValue, ok := <-c
	if !ok {
		return fmt.Errorf("channel is closed")
	}
	inValue, inType := getConcreteReflectValueAndType(firstValue) // Get the concrete type
	if err := ensureStructOrPtr(inType); err != nil {
		return err
	}
	inInnerWasPointer := inType.Kind() == reflect.Ptr
	inInnerStructInfo := getStructInfo(inType) // Get the inner struct info to get CSV annotations
	csvHeadersLabels := make([]string, len(inInnerStructInfo.Fields))
	for i, fieldInfo := range inInnerStructInfo.Fields { // Used to write the header (first line) in CSV
		csvHeadersLabels[i] = fieldInfo.getFirstKey()
	}
	if err := writer.Write(csvHeadersLabels); err != nil {
		return err
	}
	write := func(val reflect.Value) error {
		for j, fieldInfo := range inInnerStructInfo.Fields {
			csvHeadersLabels[j] = ""
			inInnerFieldValue, err := getInnerField(val, inInnerWasPointer, fieldInfo.IndexChain) // Get the correct field header <-> position
			if err != nil {
				return err
			}
			csvHeadersLabels[j] = inInnerFieldValue
		}
		if err := writer.Write(csvHeadersLabels); err != nil {
			return err
		}
		return nil
	}
	if err := write(inValue); err != nil {
		return err
	}
	for v := range c {
		val, _ := getConcreteReflectValueAndType(v) // Get the concrete type (not pointer) (Slice<?> or Array<?>)
		if err := ensureStructOrPtr(inType); err != nil {
			return err
		}
		if err := write(val); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

type headersToOmit map[string]int

func (hto headersToOmit) anyToOmit() bool {
	for _, value := range hto {
		if value != -1 {
			return true
		}
	}
	return false
}

func writeTo(writer *SafeCSVWriter, in interface{}, omitHeaders bool) error {
	inValue, inType := getConcreteReflectValueAndType(in) // Get the concrete type (not pointer) (Slice<?> or Array<?>)
	if err := ensureInType(inType); err != nil {
		return err
	}
	inInnerWasPointer, inInnerType := getConcreteContainerInnerType(inType) // Get the concrete inner type (not pointer) (Container<"?">)
	if err := ensureInInnerType(inInnerType); err != nil {
		return err
	}
	inInnerStructInfo := getStructInfo(inInnerType) // Get the inner struct info to get CSV annotations
	csvHeadersLabels := make([]string, len(inInnerStructInfo.Fields))
	csvHeadersLabelsToOmit := make(headersToOmit, len(inInnerStructInfo.Fields))

	for i, fieldInfo := range inInnerStructInfo.Fields { // Used to write the header (first line) in CSV
		if !fieldInfo.omitEmpty {
			csvHeadersLabelsToOmit[fieldInfo.getFirstKey()] = -1  // this column will never be omitted because at the end counter will never equal total number of rows
		} else {
			csvHeadersLabelsToOmit[fieldInfo.getFirstKey()] = 0
		}
		csvHeadersLabels[i] = fieldInfo.getFirstKey()
	}

	inLen := inValue.Len()

	if csvHeadersLabelsToOmit.anyToOmit() { // if some columns were marked as omitempty
		// iterate over all results to determine what headers to omit.
		for i := 0; i < inLen; i++ { // Iterate over container rows
			for _, fieldInfo := range inInnerStructInfo.Fields {
				inInnerFieldValue, err := getInnerField(inValue.Index(i), inInnerWasPointer, fieldInfo.IndexChain)
				if err != nil {
					return err
				}
				if inInnerFieldValue == "" {
					csvHeadersLabelsToOmit[fieldInfo.getFirstKey()] += 1
				}
			}
		}

		for key, emptyColumns := range csvHeadersLabelsToOmit {
			if emptyColumns == inLen { // if all rows had this column empty and column was marked to 'omitempty' -> remove the header and corresponding values from the result
				for i, header := range csvHeadersLabels {
					if key == header { // remove header
						csvHeadersLabels = append(csvHeadersLabels[:i], csvHeadersLabels[i+1:]...)
						inInnerStructInfo.Fields = append(inInnerStructInfo.Fields[:i], inInnerStructInfo.Fields[i+1:]...)
						break
					}
				}
			}
		}
	}

	if !omitHeaders {
		if err := writer.Write(csvHeadersLabels); err != nil {
			return err
		}
	}
	for i := 0; i < inLen; i++ { // Iterate over container rows
		for j, fieldInfo := range inInnerStructInfo.Fields {
			csvHeadersLabels[j] = ""
			inInnerFieldValue, err := getInnerField(inValue.Index(i), inInnerWasPointer, fieldInfo.IndexChain) // Get the correct field header <-> position
			if err != nil {
				return err
			}
			csvHeadersLabels[j] = inInnerFieldValue
		}
		if err := writer.Write(csvHeadersLabels); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func ensureStructOrPtr(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Struct:
		fallthrough
	case reflect.Ptr:
		return nil
	}
	return fmt.Errorf("cannot use " + t.String() + ", only slice or array supported")
}

// Check if the inType is an array or a slice
func ensureInType(outType reflect.Type) error {
	switch outType.Kind() {
	case reflect.Slice:
		fallthrough
	case reflect.Array:
		return nil
	}
	return fmt.Errorf("cannot use " + outType.String() + ", only slice or array supported")
}

// Check if the inInnerType is of type struct
func ensureInInnerType(outInnerType reflect.Type) error {
	switch outInnerType.Kind() {
	case reflect.Struct:
		return nil
	}
	return fmt.Errorf("cannot use " + outInnerType.String() + ", only struct supported")
}

func getInnerField(outInner reflect.Value, outInnerWasPointer bool, index []int) (string, error) {
	oi := outInner
	if outInnerWasPointer {
		oi = outInner.Elem()
	}
	return getFieldAsString(oi.FieldByIndex(index))
}
