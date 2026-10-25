// The small Windows answer file that Bootlace makes itself when the "local account" setting is on and no answer file is attached (issue #18): it only changes how the first start of
// Windows asks for an account. Nothing in it touches disks, the edition or the installation pages of Setup.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

// localAccountXML lets the first start of Windows (OOBE) make a local account without a network and without a Microsoft account. What it holds, and why (all of it seen on Windows 11
// 26H2, build 26300, in a Hyper-V VM that has no network):
//   - HideWirelessSetupInOOBE (oobeSystem) is what removes the "Let's connect you to a network" page, which has no way past it on this build (no "I don't have internet" link, and the
//     `OOBE\BYPASSNRO` command and the BypassNRO value no longer do anything: a run with the value written before OOBE still showed the page). With it OOBE goes on to "Who's going to use this
//     device?" and asks for a name and a password for a local account;
//   - HideOnlineAccountScreens (oobeSystem) leaves out the Microsoft account pages;
//   - the windowsPE part only says that the product key page is still shown: with any answer file at all Setup works in its unattended mode and stops with "Windows cannot read the
//     <ProductKey> setting" when there is neither a key nor this line. Every other page of Setup (language, edition, licence terms, disk) is still asked as without a file.
const localAccountXML = `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="windowsPE">
    <component name="Microsoft-Windows-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <UserData>
        <ProductKey>
          <WillShowUI>Always</WillShowUI>
        </ProductKey>
      </UserData>
    </component>
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <OOBE>
        <HideOnlineAccountScreens>true</HideOnlineAccountScreens>
        <HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE>
      </OOBE>
    </component>
  </settings>
</unattend>
`
