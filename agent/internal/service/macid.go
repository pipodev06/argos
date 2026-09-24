package service

import (
	"errors"
	"strconv"
	"strings"
)

// Rango de ids de usuarios de servicio en macOS que este instalador usa: por
// debajo de 500 (no aparecen en la ventana de login) y por encima de los ids que
// reserva Apple (< 300).
const (
	macIDMin = 300
	macIDMax = 399
)

// freeMacID elige el primer id libre a la vez como UniqueID de usuario y como
// PrimaryGroupID de grupo. userList y groupList son la salida de
// `dscl . -list /Users UniqueID` y `dscl . -list /Groups PrimaryGroupID`
// (una entrada "nombre id" por linea).
func freeMacID(userList, groupList string) (int, error) {
	used := map[int]bool{}
	for _, list := range []string{userList, groupList} {
		for _, line := range strings.Split(list, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if id, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				used[id] = true
			}
		}
	}
	for id := macIDMin; id <= macIDMax; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return 0, errors.New("no hay ids libres entre 300 y 399 pa el usuario de servicio")
}
