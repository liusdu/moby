package accelerator

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/docker/docker/api/server/httputils"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/filters"
	"golang.org/x/net/context"
)

const (
	MaxFilterLength  = 1024
	MaxNameLength    = 256
	MaxOptionsCount  = 128
	MaxOptionsLength = 1024
)

func (a *accelRouter) getAccelsList(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}
	filter := r.Form.Get("filters")

	if len(filter) > MaxFilterLength {
		return fmt.Errorf("Input filter length exceed limit")
	}
	accelFilters, err := filters.FromParam(filter)
	if err != nil {
		return err
	}

	accels, warnings, err := a.backend.Accels()
	if err != nil {
		return err
	}

	fAccels, err := filterAccels(accels, accelFilters)
	if err != nil {
		return err
	}

	return httputils.WriteJSON(w, http.StatusOK, &types.AccelsListResponse{Accels: fAccels, Warnings: warnings})
}

func (a *accelRouter) getAccelByName(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}

	if len(vars["name"]) > MaxNameLength {
		return fmt.Errorf("Input name length exceed limit")
	}

	accel, err := a.backend.AccelInspect(vars["name"])
	if err != nil {
		return err
	}
	return httputils.WriteJSON(w, http.StatusOK, accel)
}

func (a *accelRouter) postAccelCreate(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}

	if err := httputils.CheckForJSON(r); err != nil {
		return err
	}

	var req types.AccelCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}

	if len(req.Name) > MaxNameLength ||
		len(req.Driver) > MaxNameLength ||
		len(req.Runtime) > MaxNameLength {
		return fmt.Errorf("Input accel create parameter length exceed limit")
	}

	if len(req.Options) > MaxOptionsCount {
		return fmt.Errorf("Input accel create options count exceed limit")
	}
	for _, opt := range req.Options {
		if len(opt) > MaxOptionsLength {
			return fmt.Errorf("Input accel create option length exceed limit")
		}
	}

	accel, err := a.backend.AccelCreate(req.Name, req.Driver, req.Runtime, req.Options)
	if err != nil {
		return err
	}
	return httputils.WriteJSON(w, http.StatusCreated, accel)
}

func (a *accelRouter) deleteAccels(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}
	force := httputils.BoolValue(r, "force")

	if len(vars["name"]) > MaxNameLength {
		return fmt.Errorf("Input name length exceed limit")
	}

	if err := a.backend.AccelRm(vars["name"], force); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *accelRouter) getAccelDevices(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	drivers, _, err := a.backend.AccelDrivers()
	if err != nil {
		return err
	}

	devices := make([]types.AccelDevice, 0)

	for _, drv := range drivers {
		d, _, err := a.backend.AccelDevices(drv.Name)
		if err != nil {
			continue
		}
		devices = append(devices, d...)
	}

	return httputils.WriteJSON(w, http.StatusOK, &devices)
}

func (a *accelRouter) getAccelDevicesByDriver(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}

	if len(vars["driver"]) > MaxNameLength {
		return fmt.Errorf("Input driver length exceed limit")
	}

	devices, warnings, err := a.backend.AccelDevices(vars["driver"])
	if err != nil {
		return err
	}
	return httputils.WriteJSON(w, http.StatusOK, &types.AccelDevicesResponse{Devices: devices, Warnings: warnings})
}

func (a *accelRouter) getAccelDrivers(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	accelDrivers, warnings, err := a.backend.AccelDrivers()
	if err != nil {
		return err
	}
	return httputils.WriteJSON(w, http.StatusOK, &types.AccelDriversResponse{Drivers: accelDrivers, Warnings: warnings})
}
