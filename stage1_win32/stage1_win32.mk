WIN32_ACIDIR := $(BUILDDIR)/aci-for-win32-flavor
WIN32_ACIROOTFSDIR := $(WIN32_ACIDIR)/rootfs
WIN32_TOOLSDIR := $(TOOLSDIR)/win32
WIN32_STAMPS :=
WIN32_SUBDIRS := run gc enter aci
WIN32_STAGE1 := $(BINDIR)/stage1-win32.aci

$(call setup-stamp-file,WIN32_STAMP,aci-build)

$(call inc-many,$(foreach sd,$(WIN32_SUBDIRS),$(sd)/$(sd).mk))

$(call generate-stamp-rule,$(WIN32_STAMP),$(WIN32_STAMPS) $(ACTOOL_STAMP),, \
	$(call vb,vt,ACTOOL,$(call vsp,$(WIN32_STAGE1))) \
	"$(ACTOOL)" build --overwrite --owner-root "$(WIN32_ACIDIR)" "$(WIN32_STAGE1)")

INSTALL_DIRS += \
	$(WIN32_TOOLSDIR):- \
	$(WIN32_ACIDIR):- \
	$(WIN32_ACIROOTFSDIR):-

WIN32_FLAVORS := $(call commas-to-spaces,$(RKT_STAGE1_FLAVORS))

CLEAN_FILES += $(WIN32_STAGE1)

ifneq ($(filter win32,$(WIN32_FLAVORS)),)

# actually build the win32 stage1 only if requested

TOPLEVEL_STAMPS += $(WIN32_STAMP)

endif

$(call undefine-namespaces,WIN32 _WIN32)
