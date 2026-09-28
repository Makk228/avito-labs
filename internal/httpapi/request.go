package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/google/uuid"

	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/trip"
)

const maxBodyBytes = 64 << 10

var (
	tripDataRequired    = []string{"user_id", "driver_id", "start_point", "end_point", "price"}
	coordinatesRequired = []string{"latitude", "longitude"}
	uuidType            = reflect.TypeFor[uuid.UUID]()
)

func decodeCreateTrip(w http.ResponseWriter, r *http.Request) (trip.NewTrip, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return trip.NewTrip{}, fmt.Errorf("request body must not exceed %d bytes", tooLarge.Limit)
		}
		return trip.NewTrip{}, errors.New("failed to read request body")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return trip.NewTrip{}, errors.New("request body is required")
	}
	if !json.Valid(body) {
		return trip.NewTrip{}, errors.New("request body is not valid JSON")
	}

	if err := checkFields(body); err != nil {
		return trip.NewTrip{}, err
	}

	var req api.TripData
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return trip.NewTrip{}, decodeError(err)
	}

	if err := validateTripData(req); err != nil {
		return trip.NewTrip{}, err
	}

	return trip.NewTrip{
		UserID:   req.UserId,
		DriverID: req.DriverId,
		Start:    trip.Point{Latitude: req.StartPoint.Latitude, Longitude: req.StartPoint.Longitude},
		End:      trip.Point{Latitude: req.EndPoint.Latitude, Longitude: req.EndPoint.Longitude},
		Price:    req.Price,
	}, nil
}

func checkFields(body []byte) error {
	fields, err := objectFields(body, "request body")
	if err != nil {
		return err
	}
	if err := requireFields(fields, "", tripDataRequired); err != nil {
		return err
	}
	for _, point := range []string{"start_point", "end_point"} {
		nested, err := objectFields(fields[point], point)
		if err != nil {
			return err
		}
		if err := requireFields(nested, point+".", coordinatesRequired); err != nil {
			return err
		}
	}
	for _, name := range []string{"user_id", "driver_id"} {
		var s string
		if err := json.Unmarshal(fields[name], &s); err != nil {
			return fmt.Errorf("field %s must be a string", name)
		}
		if _, err := uuid.Parse(s); err != nil {
			return fmt.Errorf("field %s must be a valid UUID", name)
		}
	}
	return nil
}

func objectFields(data []byte, name string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	return fields, nil
}

func requireFields(fields map[string]json.RawMessage, prefix string, names []string) error {
	var missing []string
	for _, name := range names {
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			missing = append(missing, prefix+name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required fields are missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &typeErr):
		return fmt.Errorf("field %s must be of type %s", typeErr.Field, jsonType(typeErr.Type))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return fmt.Errorf("unknown field %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	default:
		return errors.New("request body does not match the TripData schema")
	}
}

func jsonType(t reflect.Type) string {
	if t == uuidType {
		return "string (uuid)"
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return t.Kind().String()
	}
}

func validateTripData(req api.TripData) error {
	var problems []string
	if req.UserId == uuid.Nil {
		problems = append(problems, "user_id must not be empty")
	}
	if req.DriverId == uuid.Nil {
		problems = append(problems, "driver_id must not be empty")
	}
	problems = append(problems, validateCoordinates("start_point", req.StartPoint)...)
	problems = append(problems, validateCoordinates("end_point", req.EndPoint)...)
	if req.Price < 0 {
		problems = append(problems, "price must be greater than or equal to 0")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validateCoordinates(name string, c api.Coordinates) []string {
	var problems []string
	if c.Latitude < -90 || c.Latitude > 90 {
		problems = append(problems, name+".latitude must be between -90 and 90")
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		problems = append(problems, name+".longitude must be between -180 and 180")
	}
	return problems
}
