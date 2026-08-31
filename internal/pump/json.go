package pump

import "encoding/json"

func ingressUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
