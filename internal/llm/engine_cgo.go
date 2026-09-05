//go:build llama

package llm

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/llama.cpp/include -I${SRCDIR}/../../third_party/llama.cpp/ggml/include -O3

// Unix paths (Linux, macOS) — CMake adds lib prefix
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a
#cgo darwin LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a

// Windows paths — CMake does not add lib prefix for nested static libs
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cpu.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cuda.a -L${SRCDIR}/../../third_party/llama.cpp/build/cuda-lib -lcudart_static -lcublas -lcublasLt -lstdc++ -lgomp -lkernel32
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a -lm -lstdc++ -lpthread -ldl -lgomp
#cgo darwin LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a -lm -lc++ -framework Accelerate

#include <stdlib.h>
#include <string.h>
#include "llama.h"

// llama_log_silent — глушим логи llama.cpp, чтобы они не смешивались с
// нашими структурированными ответами в stdout.
static void llama_log_silent(enum ggml_log_level level, const char * text, void * user_data) {
    (void) level; (void) text; (void) user_data;
}

static void dmsh_silence_logs(void) {
    llama_log_set(llama_log_silent, NULL);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

func init() {
	C.dmsh_silence_logs()
	C.llama_backend_init()
}

// cgoEngine — реальная реализация Engine на базе llama.cpp.
// Держит загруженную модель и контекст в памяти процесса.
type cgoEngine struct {
	mu     sync.Mutex
	model  *C.struct_llama_model
	ctx    *C.struct_llama_context
	vocab  *C.struct_llama_vocab
	params Params
	closed bool
}

// New загружает модель и создаёт контекст. Один Engine на процесс.
func New(p Params) (Engine, error) {
	if p.ModelPath == "" {
		return nil, errors.New("llm: empty model path")
	}
	cPath := C.CString(p.ModelPath)
	defer C.free(unsafe.Pointer(cPath))

	mparams := C.llama_model_default_params()
	if p.GPULayers > 0 {
		mparams.n_gpu_layers = C.int(p.GPULayers)
	}
	model := C.llama_model_load_from_file(cPath, mparams)
	if model == nil {
		return nil, fmt.Errorf("llm: failed to load model: %s", p.ModelPath)
	}

	cparams := C.llama_context_default_params()
	if p.CtxSize > 0 {
		cparams.n_ctx = C.uint32_t(p.CtxSize)
	}
	if p.Threads > 0 {
		cparams.n_threads = C.int32_t(p.Threads)
		cparams.n_threads_batch = C.int32_t(p.Threads)
	}

	cctx := C.llama_init_from_model(model, cparams)
	if cctx == nil {
		C.llama_model_free(model)
		return nil, errors.New("llm: failed to create llama context")
	}

	vocab := C.llama_model_get_vocab(model)
	return &cgoEngine{model: model, ctx: cctx, vocab: vocab, params: p}, nil
}

func (e *cgoEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	if e.ctx != nil {
		C.llama_free(e.ctx)
		e.ctx = nil
	}
	if e.model != nil {
		C.llama_model_free(e.model)
		e.model = nil
	}
	e.closed = true
	return nil
}

func (e *cgoEngine) Generate(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions) (string, error) {
	tokens := make(chan string, 64)
	var sb strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- e.Stream(ctx, systemPrompt, userPrompt, opts, tokens)
	}()
	for tok := range tokens {
		sb.WriteString(tok)
	}
	if err := <-done; err != nil {
		return sb.String(), err
	}
	return sb.String(), nil
}

func (e *cgoEngine) Stream(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions, out chan<- string) error {
	defer close(out)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return errors.New("llm: engine is closed")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	prompt, err := buildChatPrompt(e, systemPrompt, userPrompt)
	if err != nil {
		return err
	}

	// 1. Токенизация промпта.
	cPrompt := C.CString(prompt)
	defer C.free(unsafe.Pointer(cPrompt))
	promptLen := C.int32_t(len(prompt))

	maxTok := promptLen + 8
	tokensBuf := make([]C.llama_token, maxTok)
	n := C.llama_tokenize(e.vocab, cPrompt, promptLen, &tokensBuf[0], maxTok, C.bool(true), C.bool(true))
	if n < 0 {
		// Не хватило места — увеличиваем буфер ровно по запрошенному размеру.
		need := -n
		tokensBuf = make([]C.llama_token, need)
		n = C.llama_tokenize(e.vocab, cPrompt, promptLen, &tokensBuf[0], need, C.bool(true), C.bool(true))
		if n < 0 {
			return fmt.Errorf("llm: tokenize failed (%d)", int(n))
		}
	}
	tokens := tokensBuf[:int(n)]

	// 2. Очищаем KV-кэш (memory) для нового запроса.
	C.llama_memory_clear(C.llama_get_memory(e.ctx), C.bool(true))

	// 3. Декодируем промпт одним батчем.
	batch := C.llama_batch_get_one(&tokens[0], C.int32_t(len(tokens)))
	if rc := C.llama_decode(e.ctx, batch); rc != 0 {
		return fmt.Errorf("llm: decode prompt failed (%d)", int(rc))
	}

	// 4. Собираем sampler chain: temp + top_p + dist (или greedy при temp<=0).
	sparams := C.llama_sampler_chain_default_params()
	smpl := C.llama_sampler_chain_init(sparams)
	defer C.llama_sampler_free(smpl)

	if opts.Temperature <= 0 {
		C.llama_sampler_chain_add(smpl, C.llama_sampler_init_greedy())
	} else {
		if opts.TopP > 0 && opts.TopP < 1 {
			C.llama_sampler_chain_add(smpl, C.llama_sampler_init_top_p(C.float(opts.TopP), 1))
		}
		C.llama_sampler_chain_add(smpl, C.llama_sampler_init_temp(C.float(opts.Temperature)))
		seed := opts.Seed
		if seed == 0 {
			seed = C.LLAMA_DEFAULT_SEED
		}
		C.llama_sampler_chain_add(smpl, C.llama_sampler_init_dist(C.uint32_t(seed)))
	}

	// 5. Цикл генерации.
	maxOut := opts.MaxTokens
	if maxOut <= 0 {
		maxOut = 512
	}
	pieceBuf := make([]C.char, 256)
	var generated strings.Builder

	for i := 0; i < maxOut; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		tok := C.llama_sampler_sample(smpl, e.ctx, -1)
		if C.llama_vocab_is_eog(e.vocab, tok) {
			return nil
		}
		C.llama_sampler_accept(smpl, tok)

		nPiece := C.llama_token_to_piece(e.vocab, tok, &pieceBuf[0], C.int32_t(len(pieceBuf)), 0, C.bool(false))
		if nPiece < 0 {
			pieceBuf = make([]C.char, -nPiece)
			nPiece = C.llama_token_to_piece(e.vocab, tok, &pieceBuf[0], C.int32_t(len(pieceBuf)), 0, C.bool(false))
			if nPiece < 0 {
				return fmt.Errorf("llm: token_to_piece failed (%d)", int(nPiece))
			}
		}
		piece := C.GoStringN(&pieceBuf[0], nPiece)
		generated.WriteString(piece)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- piece:
		}

		if matchesStop(generated.String(), opts.StopTokens) {
			return nil
		}

		// Декодируем сгенерированный токен в контекст для следующего шага.
		nextBatch := C.llama_batch_get_one(&tok, 1)
		if rc := C.llama_decode(e.ctx, nextBatch); rc != 0 {
			return fmt.Errorf("llm: decode step failed (%d)", int(rc))
		}
	}
	return nil
}

// buildChatPrompt форматирует system+user в промпт чата. Сначала пробует
// использовать встроенный chat template модели (через llama_chat_apply_template),
// чтобы корректно работать с моделями без ChatML-разметки. Если шаблон
// отсутствует или применение не удалось — откат на ChatML.
func buildChatPrompt(e *cgoEngine, system, user string) (string, error) {
	tmpl := C.llama_model_chat_template(e.model, nil)
	if tmpl == nil {
		return fallbackChatML(system, user), nil
	}

	msgs := make([]C.struct_llama_chat_message, 0, 2)
	var allocs []*C.char
	defer func() {
		for _, p := range allocs {
			C.free(unsafe.Pointer(p))
		}
	}()
	addMsg := func(role, content string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		cRole := C.CString(role)
		cContent := C.CString(content)
		allocs = append(allocs, cRole, cContent)
		msgs = append(msgs, C.struct_llama_chat_message{role: cRole, content: cContent})
	}
	addMsg("system", system)
	addMsg("user", user)
	if len(msgs) == 0 {
		return "", errors.New("llm: empty prompt")
	}

	size := C.int32_t(2048)
	buf := make([]C.char, size)
	n := C.llama_chat_apply_template(tmpl, &msgs[0], C.size_t(len(msgs)), C.bool(true), &buf[0], size)
	if n < 0 {
		return "", fmt.Errorf("llm: apply chat template failed (%d)", int(n))
	}
	if n >= size {
		size = n + 1
		buf = make([]C.char, int(size))
		n = C.llama_chat_apply_template(tmpl, &msgs[0], C.size_t(len(msgs)), C.bool(true), &buf[0], size)
		if n < 0 {
			return "", fmt.Errorf("llm: apply chat template failed (%d)", int(n))
		}
	}
	if n <= 0 {
		return fallbackChatML(system, user), nil
	}
	return C.GoStringN(&buf[0], n), nil
}

// fallbackChatML — жёстко заданный ChatML-формат для моделей без встроенного
// шаблона (и для stub/тестов). Историческое поведение до внедрения
// llama_chat_apply_template.
func fallbackChatML(system, user string) string {
	var b strings.Builder
	if strings.TrimSpace(system) != "" {
		b.WriteString("<|im_start|>system\n")
		b.WriteString(system)
		b.WriteString("<|im_end|>\n")
	}
	b.WriteString("<|im_start|>user\n")
	b.WriteString(user)
	b.WriteString("<|im_end|>\n")
	b.WriteString("<|im_start|>assistant\n")
	return b.String()
}

func matchesStop(generated string, stops []string) bool {
	for _, s := range stops {
		if s != "" && strings.Contains(generated, s) {
			return true
		}
	}
	return false
}
