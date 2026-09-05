//go:build llama && cuda

package llm

/*
// CUDA build (GGML_CUDA=ON). Links all ggml static libs plus ggml-cuda.a
// and the CUDA toolkit runtime libraries.
//
// Linux statically links cudart/cublas/cublasLt (available in the toolkit);
// Windows links cudart_static + dynamic cublas, and needs the CUDA DLLs
// (cublas64_*.dll) shipped alongside the binary at runtime.
//
// The CUDA toolkit location is injected via CGO_LDFLAGS from the CI job to
// avoid hardcoding a versioned path here.

#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cuda.a -lm -lstdc++ -lpthread -ldl -lgomp -lcudart_static -lcublas_static -lcublasLt_static -lcuda -lrt -ldl -lpthread
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cpu.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cuda.a -lstdc++ -lgomp -lcudart -lcublas -lcuda
#cgo darwin LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cuda.a -lm -lc++ -framework Accelerate
*/
import "C"
