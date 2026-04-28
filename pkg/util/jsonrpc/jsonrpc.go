// Copyright (C) 2025 wangyusong
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/glidea/zenfeed/pkg/api"
)

type Handler[Request any, Response any] func(ctx context.Context, req *Request) (*Response, error)

func API[Request any, Response any](handler Handler[Request, Response]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if r.Body != http.NoBody {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

				return
			}
		}

		resp, err := handler(r.Context(), &req)
		if err != nil {
			var apiErr api.Error
			if errors.As(err, &apiErr) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(apiErr.Code)
				_ = json.NewEncoder(w).Encode(apiErr)

				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}
	})
}
