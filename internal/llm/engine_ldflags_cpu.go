//go:build llama && !cuda

package llm

/*
// Unix paths (Linux, macOS) — CMake adds lib prefix
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a
#cgo darwin LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a

// Windows paths — CMake does not add lib prefix for nested static libs
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cpu.a -lstdc++ -lgomp
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a -lm -lstdc++ -lpthread -ldl -lgomp
#cgo darwin LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a -lm -lc++ -framework Accelerate
*/
import "C"
