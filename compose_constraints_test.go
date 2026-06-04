package main_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// composeFile é resolvido relativo ao diretório do teste, que é a raiz do módulo.
const composeFile = "docker-compose.yml"

// Limites impostos pela spec da Rinha de Backend 2026.
const (
	maxCPUTotal    = 1.0
	maxMemoryBytes = 350 * 1_000_000
)

// --- Structs de parsing ---

type compose struct {
	Services map[string]service `yaml:"services"`
	Networks map[string]network `yaml:"networks"`
}

type service struct {
	Image       string      `yaml:"image"`
	Ports       []string    `yaml:"ports"`
	NetworkMode string      `yaml:"network_mode"`
	Privileged  bool        `yaml:"privileged"`
	CPUPeriod   int64       `yaml:"cpu_period"`
	CPUQuota    int64       `yaml:"cpu_quota"`
	Deploy      deploy      `yaml:"deploy"`
	Networks    any         `yaml:"networks"`
}

type deploy struct {
	Resources resources `yaml:"resources"`
}

type resources struct {
	Limits limits `yaml:"limits"`
}

type limits struct {
	CPUs   string `yaml:"cpus"`
	Memory string `yaml:"memory"`
}

type network struct {
	Driver string `yaml:"driver"`
}

func loadCompose(t *testing.T) compose {
	t.Helper()
	data, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatalf("não foi possível ler %s: %v", composeFile, err)
	}
	var c compose
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatalf("docker-compose.yml inválido: %v", err)
	}
	return c
}

// serviceCPU retorna a fração de CPU alocada para um serviço.
// Suporta cpu_period/cpu_quota (campos top-level) e deploy.resources.limits.cpus.
func serviceCPU(s service) float64 {
	if s.CPUPeriod > 0 && s.CPUQuota > 0 {
		return float64(s.CPUQuota) / float64(s.CPUPeriod)
	}
	if s.Deploy.Resources.Limits.CPUs != "" {
		v, err := strconv.ParseFloat(s.Deploy.Resources.Limits.CPUs, 64)
		if err == nil {
			return v
		}
	}
	return 0
}

// serviceMemoryBytes converte a string de memória do compose para bytes.
// Suporta os sufixos: B, K/KB/KiB, M/MB/MiB, G/GB/GiB (case-insensitive).
func serviceMemoryBytes(s service) (int64, error) {
	raw := s.Deploy.Resources.Limits.Memory
	if raw == "" {
		return 0, nil
	}
	return parseMemory(raw)
}

func parseMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	upper := strings.ToUpper(s)

	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"GIB", 1 << 30}, {"GB", 1000 * 1000 * 1000}, {"G", 1 << 30},
		{"MIB", 1 << 20}, {"MB", 1000 * 1000}, {"M", 1 << 20},
		{"KIB", 1 << 10}, {"KB", 1000}, {"K", 1 << 10},
		{"B", 1},
	}
	for _, sf := range suffixes {
		if strings.HasSuffix(upper, sf.suffix) {
			numStr := s[:len(s)-len(sf.suffix)]
			v, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
			if err != nil {
				return 0, fmt.Errorf("valor inválido %q: %w", s, err)
			}
			return int64(v * float64(sf.mult)), nil
		}
	}
	// sem sufixo: assume bytes
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("formato de memória desconhecido: %q", s)
	}
	return v, nil
}

// isLoadBalancer heurística: serviço com porta 9999 exposta é o LB.
func isLoadBalancer(s service) bool {
	for _, p := range s.Ports {
		if strings.Contains(p, "9999") {
			return true
		}
	}
	return false
}

// --- Testes ---

func TestComposePort9999Exposed(t *testing.T) {
	c := loadCompose(t)
	for name, svc := range c.Services {
		for _, p := range svc.Ports {
			if strings.HasPrefix(p, "9999:") || p == "9999" {
				t.Logf("porta 9999 exposta pelo serviço %q", name)
				return
			}
		}
	}
	t.Fatal("nenhum serviço expõe a porta 9999")
}

func TestComposeAtLeastTwoAPIInstances(t *testing.T) {
	c := loadCompose(t)

	// Identifica a imagem da API (serviços sem porta 9999).
	var apiImage string
	var apiCount int
	for _, svc := range c.Services {
		if !isLoadBalancer(svc) {
			if apiImage == "" {
				apiImage = svc.Image
			}
			if svc.Image == apiImage {
				apiCount++
			}
		}
	}
	if apiCount < 2 {
		t.Errorf("esperado ≥ 2 instâncias da API (imagem %q), encontrado %d", apiImage, apiCount)
	}
}

// TestComposeLBImageDifferentFromAPI garante que o load balancer não usa a mesma
// imagem que a API — proxy para "LB não aplica lógica de detecção".
func TestComposeLBImageDifferentFromAPI(t *testing.T) {
	c := loadCompose(t)

	var apiImage string
	for _, svc := range c.Services {
		if !isLoadBalancer(svc) {
			apiImage = svc.Image
			break
		}
	}
	for name, svc := range c.Services {
		if isLoadBalancer(svc) && svc.Image == apiImage {
			t.Errorf("serviço %q (LB) usa a mesma imagem da API (%s); o LB não deve rodar lógica de detecção", name, apiImage)
		}
	}
}

func TestComposeBridgeNetwork(t *testing.T) {
	c := loadCompose(t)
	if len(c.Networks) == 0 {
		t.Fatal("nenhuma network declarada no compose")
	}
	for name, net := range c.Networks {
		driver := net.Driver
		if driver == "" {
			driver = "bridge" // driver padrão do Docker Compose
		}
		if driver != "bridge" {
			t.Errorf("network %q usa driver %q; apenas bridge é permitido", name, driver)
		}
	}
}

func TestComposeNoHostOrPrivileged(t *testing.T) {
	c := loadCompose(t)
	for name, svc := range c.Services {
		if svc.NetworkMode == "host" {
			t.Errorf("serviço %q usa network_mode: host, que não é permitido", name)
		}
		if svc.Privileged {
			t.Errorf("serviço %q usa privileged: true, que não é permitido", name)
		}
	}
}

func TestComposeCPULimit(t *testing.T) {
	c := loadCompose(t)
	var total float64
	for name, svc := range c.Services {
		cpu := serviceCPU(svc)
		if cpu == 0 {
			t.Errorf("serviço %q não declara limite de CPU (cpu_period/cpu_quota ou deploy.resources.limits.cpus)", name)
			continue
		}
		t.Logf("  %s: %.4f CPU", name, cpu)
		total += cpu
	}
	t.Logf("total de CPU: %.4f (limite: %.1f)", total, maxCPUTotal)
	if total > maxCPUTotal+1e-9 {
		t.Errorf("soma dos limites de CPU %.4f excede o máximo permitido de %.1f", total, maxCPUTotal)
	}
}

func TestComposeMemoryLimit(t *testing.T) {
	c := loadCompose(t)
	var totalBytes int64
	for name, svc := range c.Services {
		mem, err := serviceMemoryBytes(svc)
		if err != nil {
			t.Errorf("serviço %q: %v", name, err)
			continue
		}
		if mem == 0 {
			t.Errorf("serviço %q não declara limite de memória em deploy.resources.limits.memory", name)
			continue
		}
		t.Logf("  %s: %d MB", name, mem/1_000_000)
		totalBytes += mem
	}
	t.Logf("total de memória: %d MB (limite: %d MB)", totalBytes/1_000_000, maxMemoryBytes/1_000_000)
	if totalBytes > maxMemoryBytes {
		t.Errorf("soma dos limites de memória %d MB excede o máximo permitido de %d MB",
			totalBytes/1_000_000, maxMemoryBytes/1_000_000)
	}
}
