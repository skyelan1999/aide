/* Native sanitizer/fault harness. Compiled only by test-native.py, never shipped
 * as part of the decoder. Source and fixture identities are checked by runner. */
#include "opj_includes.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static unsigned fault_site, fault_hits, capture_enabled, use_file_stream;
static opj_tcd_cblk_dec_t captured;
static unsigned captured_style, captured_orientation;

void *aide_fault_realloc(void *pointer, size_t size, unsigned site)
{
    if (pointer && site == fault_site && fault_hits++ == 0) return NULL;
    return opj_realloc(pointer, size);
}

void aide_capture_ht(opj_tcd_cblk_dec_t *block, unsigned style, unsigned orientation)
{
    unsigned i;
    if (!capture_enabled || captured.chunks) return;
    captured = *block;
    captured.decoded_data = NULL;
    captured.segs = opj_calloc(block->m_current_max_segs, sizeof(*captured.segs));
    captured.chunks = opj_calloc(block->numchunks, sizeof(*captured.chunks));
    if (!captured.segs || !captured.chunks) abort();
    memcpy(captured.segs, block->segs, block->m_current_max_segs * sizeof(*captured.segs));
    for (i = 0; i < block->numchunks; ++i) {
        captured.chunks[i].len = block->chunks[i].len;
        captured.chunks[i].data = opj_calloc(block->chunks[i].len + OPJ_COMMON_CBLK_DATA_EXTRA, 1);
        if (!captured.chunks[i].data) abort();
        memcpy(captured.chunks[i].data, block->chunks[i].data, block->chunks[i].len);
    }
    captured_style = style;
    captured_orientation = orientation;
}

static void message(const char *text, void *data)
{
    (void)text; (void)data;
}

static int decode(const char *filename, int expected_failure)
{
    FILE *file = fopen(filename, "rb");
    long length;
    unsigned char *data;
    opj_buffer_info_t buffer;
    opj_stream_t *stream;
    opj_codec_t *codec;
    opj_image_t *image = NULL;
    opj_dparameters_t options;
    OPJ_BOOL result;
    unsigned c;
    if (!file || fseek(file, 0, SEEK_END) || (length = ftell(file)) < 12 ||
            fseek(file, 0, SEEK_SET)) abort();
    data = opj_malloc((size_t)length);
    if (!data || fread(data, 1, (size_t)length, file) != (size_t)length) abort();
    fclose(file);
    buffer.buf = buffer.cur = data;
    buffer.len = (size_t)length;
    stream = use_file_stream ? opj_stream_create_default_file_stream(filename, OPJ_TRUE) :
             opj_stream_create_buffer_stream(&buffer, OPJ_TRUE);
    codec = opj_create_decompress(data[0] == 255 ? OPJ_CODEC_J2K : OPJ_CODEC_JP2);
    if (!stream || !codec) abort();
    opj_set_info_handler(codec, message, NULL);
    opj_set_warning_handler(codec, message, NULL);
    opj_set_error_handler(codec, message, NULL);
    opj_set_default_decoder_parameters(&options);
    result = opj_setup_decoder(codec, &options) && opj_read_header(stream, codec, &image) &&
             opj_decode(codec, stream, image) && opj_end_decompress(codec, stream);
    printf("{\"decoded\":%s,\"first\":[", result ? "true" : "false");
    if (result) for (c = 0; c < image->numcomps; ++c) {
        if (c) printf(",");
        printf("%d", image->comps[c].data[0]);
    }
    printf("],\"faultHits\":%u}\n", fault_hits);
    opj_destroy_codec(codec);
    opj_stream_destroy(stream);
    opj_image_destroy(image);
    opj_free(data);
    return (!!result == !expected_failure) ? 0 : 1;
}

extern OPJ_BOOL opj_t1_ht_decode_cblk(opj_t1_t *, opj_tcd_cblk_dec_t *,
    OPJ_UINT32, OPJ_UINT32, OPJ_UINT32, opj_event_mgr_t *, opj_mutex_t *, OPJ_BOOL);

static int heights(const char *filename)
{
    unsigned h, i;
    opj_event_mgr_t events;
    capture_enabled = 1;
    if (decode(filename, 0)) return 1;
    capture_enabled = 0;
    if (!captured.chunks || captured.real_num_segs < 2) return 2;
    memset(&events, 0, sizeof(events));
    events.error_handler = events.warning_handler = events.info_handler = message;
    for (h = 1; h <= 64; ++h) {
        opj_t1_t *t1 = opj_t1_create(OPJ_FALSE);
        opj_tcd_cblk_dec_t block = captured;
        if (!t1) abort();
        block.y1 = block.y0 + (OPJ_INT32)h;
        /* Mutating the block height makes most entropy inputs non-conforming.
         * Success is not required; every accepted/rejected case must be bounded. */
        (void)opj_t1_ht_decode_cblk(t1, &block, captured_orientation, 0,
                                   captured_style, &events, NULL, OPJ_FALSE);
        opj_t1_destroy(t1);
    }
    for (i = 0; i < captured.numchunks; ++i) opj_free(captured.chunks[i].data);
    opj_free(captured.chunks);
    opj_free(captured.segs);
    printf("{\"htHeightCases\":64}\n");
    return 0;
}

int main(int argc, char **argv)
{
    int i, expected_failure = 0, result = 0;
    if (argc < 3) return 2;
    if (!strcmp(argv[1], "heights")) return heights(argv[2]);
    if (!strcmp(argv[1], "fault")) {
        if (argc != 4) return 2;
        fault_site = (unsigned)atoi(argv[2]);
        for (i = 0; i < 25; ++i) {
            fault_hits = 0;
            if (decode(argv[3], 1) || !fault_hits) return 3;
        }
        return 0;
    }
    if (!strcmp(argv[1], "file") || !strcmp(argv[1], "file-reject")) use_file_stream = 1;
    if (!strcmp(argv[1], "reject") || !strcmp(argv[1], "file-reject")) expected_failure = 1;
    for (i = 2; i < argc; ++i) result |= decode(argv[i], expected_failure);
    return result;
}
