// Local static configuration: compile only the unmodified upstream tinfl core.
#pragma once
#define MINIZ_NO_MALLOC
#define MINIZ_NO_STDIO
#define MINIZ_NO_TIME
#define MINIZ_USE_UNALIGNED_LOADS_AND_STORES 0
#define MINIZ_LITTLE_ENDIAN 0
#define MINIZ_HAS_64BIT_REGISTERS 1
#include "miniz_tinfl.h"
