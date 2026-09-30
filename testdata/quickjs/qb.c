/*
 * qb.c: the measurement harness around quickjs-ng. The same file is compiled
 * natively (driven by qb_native.c) and to WebAssembly (driven from Go through
 * the translated package, or through wazero), so every engine runs the same
 * embedding code.
 *
 * Import: env.host_print(p, n) receives print()/console.log output.
 */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "quickjs.h"

#ifdef __wasm__
#define QB_IMPORT(n) __attribute__((import_module("env"), import_name(n)))
#define QB_EXPORT(n) __attribute__((export_name(n)))
#else
#define QB_IMPORT(n)
#define QB_EXPORT(n) __attribute__((visibility("default")))
#endif

QB_IMPORT("host_print") void host_print(const char *p, int n);

static char *out_buf;
static size_t out_len, out_cap;

static void out_reset(void) { out_len = 0; if (out_buf) out_buf[0] = 0; }
static void out_append(const char *s, size_t n)
{
    if (out_len + n + 1 > out_cap) {
        size_t c = out_cap ? out_cap : 256;
        while (out_len + n + 1 > c) c *= 2;
        char *b = realloc(out_buf, c);
        if (!b) return;
        out_buf = b; out_cap = c;
    }
    memcpy(out_buf + out_len, s, n);
    out_len += n;
    out_buf[out_len] = 0;
}

QB_EXPORT("qb_out") const char *qb_out(void) { return out_buf ? out_buf : ""; }
QB_EXPORT("qb_out_len") int qb_out_len(void) { return (int)out_len; }
QB_EXPORT("qb_malloc") void *qb_malloc(int n) { return malloc(n); }
QB_EXPORT("qb_free") void qb_free(void *p) { free(p); }

static void out_value(JSContext *ctx, JSValueConst v)
{
    size_t n;
    const char *s = JS_ToCStringLen(ctx, &n, v);
    if (s) { out_append(s, n); JS_FreeCString(ctx, s); }
    else { JS_FreeValue(ctx, JS_GetException(ctx)); out_append("<unprintable>", 13); }
}

static void out_exception(JSContext *ctx)
{
    JSValue e = JS_GetException(ctx);
    out_value(ctx, e);
    JS_FreeValue(ctx, e);
}

static JSValue js_print(JSContext *ctx, JSValueConst this_val, int argc, JSValueConst *argv)
{
    for (int i = 0; i < argc; i++) {
        size_t n;
        const char *s = JS_ToCStringLen(ctx, &n, argv[i]);
        if (!s) return JS_EXCEPTION;
        if (i) host_print(" ", 1);
        host_print(s, (int)n);
        JS_FreeCString(ctx, s);
    }
    host_print("\n", 1);
    return JS_UNDEFINED;
}

/* stack_size < 0 keeps QuickJS's default (JS_DEFAULT_STACK_SIZE). */
QB_EXPORT("qb_new") JSContext *qb_new(int stack_size)
{
    JSRuntime *rt = JS_NewRuntime();
    if (!rt) return NULL;
    if (stack_size >= 0) JS_SetMaxStackSize(rt, stack_size);
    JSContext *ctx = JS_NewContext(rt);
    if (!ctx) return NULL;
    JSValue g = JS_GetGlobalObject(ctx);
    JSValue pr = JS_NewCFunction(ctx, js_print, "print", 1);
    JS_SetPropertyStr(ctx, g, "print", JS_DupValue(ctx, pr));
    JSValue con = JS_NewObject(ctx);
    JS_SetPropertyStr(ctx, con, "log", pr);
    JS_SetPropertyStr(ctx, g, "console", con);
    JS_FreeValue(ctx, g);
    return ctx;
}

QB_EXPORT("qb_free_ctx") void qb_free_ctx(JSContext *ctx)
{
    JSRuntime *rt = JS_GetRuntime(ctx);
    JS_FreeContext(ctx);
    JS_FreeRuntime(rt);
}

/* Evaluates a global script. Returns 0 with String(result) in qb_out, or 1
   with the exception's text in qb_out. */
QB_EXPORT("qb_eval") int qb_eval(JSContext *ctx, const char *src, int len, const char *name)
{
    out_reset();
    JSValue v = JS_Eval(ctx, src, len, name, JS_EVAL_TYPE_GLOBAL);
    if (JS_IsException(v)) { out_exception(ctx); return 1; }
    out_value(ctx, v);
    JS_FreeValue(ctx, v);
    return 0;
}

/* Calls the global function `name` with no arguments. Returns 0 with
   String(result) in qb_out, or 1 with the exception's text. */
QB_EXPORT("qb_call") int qb_call(JSContext *ctx, const char *name)
{
    out_reset();
    JSValue g = JS_GetGlobalObject(ctx);
    JSValue f = JS_GetPropertyStr(ctx, g, name);
    JS_FreeValue(ctx, g);
    JSValue v = JS_Call(ctx, f, JS_UNDEFINED, 0, NULL);
    JS_FreeValue(ctx, f);
    if (JS_IsException(v)) { out_exception(ctx); return 1; }
    out_value(ctx, v);
    JS_FreeValue(ctx, v);
    for (;;) {
        JSContext *c1;
        int r = JS_ExecutePendingJob(JS_GetRuntime(ctx), &c1);
        if (r == 0) break;
        if (r < 0) { out_reset(); out_exception(c1); return 1; }
    }
    return 0;
}

QB_EXPORT("qb_memory_used") double qb_memory_used(JSContext *ctx)
{
    JSMemoryUsage u;
    JS_ComputeMemoryUsage(JS_GetRuntime(ctx), &u);
    return (double)u.memory_used_size;
}
