package server

import "github.com/gofiber/fiber/v2"

type apiError struct {
	Message string `json:"message"`
}

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(apiError{Message: msg})
}

func notFound(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusNotFound).JSON(apiError{Message: msg})
}

func internalError(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusInternalServerError).JSON(apiError{Message: msg})
}
