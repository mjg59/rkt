INIT_FLAVORS := $(filter-out win32,$(STAGE1_FLAVORS))

ifneq ($(INIT_FLAVORS),)
include stage1/makelib/aci_simple_go_bin.mk
endif
