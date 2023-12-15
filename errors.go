package bifrost

import "fmt"

type NoRouteError bool

func (e NoRouteError) Error() string {
	return fmt.Sprintf("no route found")
}
