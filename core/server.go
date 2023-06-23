package core

import (
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
