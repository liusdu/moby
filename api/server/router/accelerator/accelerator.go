package accelerator

import "github.com/docker/docker/api/server/router"

// accelRouter is a router to talk with the accelerator controller
type accelRouter struct {
	backend Backend
	routes  []router.Route
}

// NewRouter initializes a new volume router
func NewRouter(b Backend) router.Router {
	r := &accelRouter{
		backend: b,
	}
	r.initRoutes()
	return r
}

// Routes returns the available routes to the volumes controller
func (r *accelRouter) Routes() []router.Route {
	return r.routes
}

func (r *accelRouter) initRoutes() {
	r.routes = []router.Route{
		// GET
		router.NewGetRoute("/accelerators/devices", r.getAccelDevices),
		router.NewGetRoute("/accelerators/drivers", r.getAccelDrivers),
		router.NewGetRoute("/accelerators/drivers/{driver:.*}/devices", r.getAccelDevicesByDriver),
		router.NewGetRoute("/accelerators/slots", r.getAccelsList),
		router.NewGetRoute("/accelerators/slots/{name:.*}", r.getAccelByName),
		// POST
		router.NewPostRoute("/accelerators/slots/create", r.postAccelCreate),
		// DELETE
		router.NewDeleteRoute("/accelerators/slots/{name:.*}", r.deleteAccels),
	}
}
