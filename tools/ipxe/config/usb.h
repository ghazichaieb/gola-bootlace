/* UEFI only: leave the USB controllers to the firmware.
 *
 * With iPXE's own EHCI/XHCI/UHCI drivers the firmware's USB keyboard driver lets go of the controller when iPXE starts, and the keyboard is dead in the menu
 * (ASUS ROG X299-E Gaming II, AMI firmware: the keyboard works in the BIOS and the F12 menu, not in the stock iPXE, and works again without these drivers).
 * The network cards of the firmware and iPXE's own PCI drivers are not affected; a USB network adapter is then only usable through the firmware's own
 * driver (snponly.efi). The BIOS builds keep the defaults.
 */
#ifdef PLATFORM_efi
#undef USB_HCD_EHCI
#undef USB_HCD_UHCI
#undef USB_HCD_XHCI
#endif
