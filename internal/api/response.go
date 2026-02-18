package api

import "github.com/gofiber/fiber/v2"

// Envelope is the standard JSON response wrapper for all successful API
// responses. Every response body has the shape:
//
//	{
//	  "data": <payload>,
//	  "meta": { ...trust fields... }
//	}
type Envelope struct {
	Data any      `json:"data"`
	Meta TrustMeta `json:"meta"`
}

// OK writes a 200 OK JSON response with the standard envelope.
// Use this for all successful read endpoints.
func OK(c *fiber.Ctx, data any, meta TrustMeta) error {
	return c.Status(fiber.StatusOK).JSON(Envelope{
		Data: data,
		Meta: meta,
	})
}

// Created writes a 201 Created JSON response with the standard envelope.
func Created(c *fiber.Ctx, data any, meta TrustMeta) error {
	return c.Status(fiber.StatusCreated).JSON(Envelope{
		Data: data,
		Meta: meta,
	})
}
