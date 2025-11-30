package server

import "github.com/gofiber/fiber/v2"

// notImplemented is a temporary placeholder for API routes.
func notImplemented(c *fiber.Ctx) error {
	return fiber.NewError(fiber.StatusNotImplemented, "not implemented")
}
