package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type SearchEngine interface {
	Search(Query) ([]SearchResult, error)
	IsInitialized() bool
	Name() string
}

type Server struct {
	app  *fiber.App
	addr string
}

func NewServer(host string, port int, searchEngines ...SearchEngine) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	serv := Server{
		app:  fiber.New(),
		addr: addr,
	}

	for _, engine := range searchEngines {
		locEngine := engine
		serv.app.Get(fmt.Sprintf("/%s/search", strings.ToLower(locEngine.Name())), func(c *fiber.Ctx) error {
			q := Query{}
			err := q.InitFromContext(c)

			if err != nil {
				logrus.Errorf("Error while setting %s query: %s", locEngine.Name(), err)
				return err
			}

			res, err := locEngine.Search(q)
			if err != nil {
				switch err {
				case ErrCaptcha:
					err = errors.New(fmt.Sprintf("Captcha found, please stop sending requests for a while\n%s", err))
					c.Status(503)
				default:
					c.Status(500)
				}

				logrus.Errorf("Error during %s search: %s", locEngine.Name(), err)
				return err
			}

			return c.JSON(res)
		})
	}

	return &serv
}

func (s *Server) Listen() error {
	return s.app.Listen(s.addr)
}

func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}
