<#
Creates a Start Menu shortcut with its System.AppUserModel.ID property
stamped in the SAME COM session as creation (not a Load-after-the-fact
on a shortcut some other tool created) — verified live, repeatedly:
loading an EXISTING .lnk (e.g. one NSIS's own CreateShortcut just made)
and then writing IPropertyStore.SetValue on it is unreliable (IPersistFile::Load
intermittently returns S_FALSE with a degraded/near-empty property
store, and writes silently don't persist despite every call reporting
S_OK) — while creating fresh + setting the property + saving, all
against one IShellLinkW instance, persists reliably every time. GUIDs
and vtable orders per Microsoft's IShellLinkW/IPersistFile/
IPropertyStore documentation.

project.nsi invokes this with NO -params at all — it sets the
CATCHM_AUMID_* environment variables (inherited by this child process)
instead, deliberately never interpolating $INSTDIR/$SMPROGRAMS (user-
influenced — the directory-picker page lets someone type a path ending
in "\") into a quoted command line: CommandLineToArgvW treats a
backslash immediately before the closing quote as an escaped literal
quote rather than the terminator, which can merge/mis-split arguments.
Environment variable values need no shell-style quoting at all. The
explicit parameters below only exist for standalone/manual testing.
#>
param(
    [string]$ShortcutPath = $env:CATCHM_AUMID_SHORTCUT_PATH,
    [string]$TargetPath = $env:CATCHM_AUMID_TARGET_PATH,
    [string]$WorkingDirectory = $env:CATCHM_AUMID_WORKING_DIR,
    [string]$IconPath = $env:CATCHM_AUMID_ICON_PATH,
    [string]$AppUserModelID = $env:CATCHM_AUMID_APP_ID
)

foreach ($required in @('ShortcutPath', 'TargetPath', 'AppUserModelID')) {
    if ([string]::IsNullOrEmpty((Get-Variable -Name $required).Value)) {
        throw "New-AumidShortcut.ps1: -$required is required (or its CATCHM_AUMID_* environment variable)."
    }
}

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

[ComImport]
[Guid("00021401-0000-0000-C000-000000000046")]
[ClassInterface(ClassInterfaceType.None)]
public class ShellLink { }

[ComImport]
[Guid("000214F9-0000-0000-C000-000000000046")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IShellLinkW
{
    [PreserveSig] uint GetPath(IntPtr pszFile, int cchMaxPath, IntPtr pfd, uint fFlags);
    [PreserveSig] uint GetIDList(out IntPtr ppidl);
    [PreserveSig] uint SetIDList(IntPtr pidl);
    [PreserveSig] uint GetDescription(IntPtr pszName, int cchMaxName);
    [PreserveSig] uint SetDescription([MarshalAs(UnmanagedType.LPWStr)] string pszName);
    [PreserveSig] uint GetWorkingDirectory(IntPtr pszDir, int cchMaxPath);
    [PreserveSig] uint SetWorkingDirectory([MarshalAs(UnmanagedType.LPWStr)] string pszDir);
    [PreserveSig] uint GetArguments(IntPtr pszArgs, int cchMaxPath);
    [PreserveSig] uint SetArguments([MarshalAs(UnmanagedType.LPWStr)] string pszArgs);
    [PreserveSig] uint GetHotkey(out ushort pwHotkey);
    [PreserveSig] uint SetHotkey(ushort wHotKey);
    [PreserveSig] uint GetShowCmd(out int piShowCmd);
    [PreserveSig] uint SetShowCmd(int iShowCmd);
    [PreserveSig] uint GetIconLocation(IntPtr pszIconPath, int cchIconPath, out int piIcon);
    [PreserveSig] uint SetIconLocation([MarshalAs(UnmanagedType.LPWStr)] string pszIconPath, int iIcon);
    [PreserveSig] uint SetRelativePath([MarshalAs(UnmanagedType.LPWStr)] string pszPathRel, uint dwReserved);
    [PreserveSig] uint Resolve(IntPtr hwnd, uint fFlags);
    [PreserveSig] uint SetPath([MarshalAs(UnmanagedType.LPWStr)] string pszFile);
}

[ComImport]
[Guid("0000010b-0000-0000-C000-000000000046")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IPersistFile
{
    [PreserveSig] uint GetClassID(out Guid pClassID);
    [PreserveSig] uint IsDirty();
    [PreserveSig] uint Load([MarshalAs(UnmanagedType.LPWStr)] string pszFileName, uint dwMode);
    [PreserveSig] uint Save([MarshalAs(UnmanagedType.LPWStr)] string pszFileName, [MarshalAs(UnmanagedType.Bool)] bool fRemember);
    [PreserveSig] uint SaveCompleted([MarshalAs(UnmanagedType.LPWStr)] string pszFileName);
    [PreserveSig] uint GetCurFile([MarshalAs(UnmanagedType.LPWStr)] out string ppszFileName);
}

[StructLayout(LayoutKind.Sequential, Pack = 4)]
public struct PROPERTYKEY
{
    public Guid fmtid;
    public uint pid;
    public PROPERTYKEY(Guid f, uint p) { fmtid = f; pid = p; }
}

[StructLayout(LayoutKind.Explicit)]
public struct PROPVARIANT
{
    [FieldOffset(0)] public ushort vt;
    [FieldOffset(8)] public IntPtr pszVal;
}

[ComImport]
[Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IPropertyStore
{
    [PreserveSig] uint GetCount(out uint cProps);
    [PreserveSig] uint GetAt(uint iProp, out PROPERTYKEY pkey);
    [PreserveSig] uint GetValue(ref PROPERTYKEY key, ref PROPVARIANT pv);
    [PreserveSig] uint SetValue(ref PROPERTYKEY key, ref PROPVARIANT pv);
    [PreserveSig] uint Commit();
}

public static class AumidShortcut
{
    private static bool Failed(uint hr) { return (hr & 0x80000000) != 0; }

    public static void Create(string shortcutPath, string targetPath, string workingDirectory, string iconPath, string appUserModelId)
    {
        var link = (IShellLinkW)new ShellLink();

        uint hr = link.SetPath(targetPath);
        if (Failed(hr)) throw new COMException(string.Format("SetPath failed: 0x{0:X8}", hr), (int)hr);

        if (!string.IsNullOrEmpty(workingDirectory))
        {
            hr = link.SetWorkingDirectory(workingDirectory);
            if (Failed(hr)) throw new COMException(string.Format("SetWorkingDirectory failed: 0x{0:X8}", hr), (int)hr);
        }

        if (!string.IsNullOrEmpty(iconPath))
        {
            hr = link.SetIconLocation(iconPath, 0);
            if (Failed(hr)) throw new COMException(string.Format("SetIconLocation failed: 0x{0:X8}", hr), (int)hr);
        }

        var ps = (IPropertyStore)link;
        var key = new PROPERTYKEY(new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3"), 5); // PKEY_AppUserModel_ID
        var pv = new PROPVARIANT();
        pv.vt = 31; // VT_LPWSTR
        pv.pszVal = Marshal.StringToCoTaskMemUni(appUserModelId);

        hr = ps.SetValue(ref key, ref pv);
        if (Failed(hr))
        {
            pv.pszVal = IntPtr.Zero;
            throw new COMException(string.Format("SetValue(AUMID) failed: 0x{0:X8}", hr), (int)hr);
        }

        hr = ps.Commit();
        if (Failed(hr)) throw new COMException(string.Format("Commit failed: 0x{0:X8}", hr), (int)hr);

        Marshal.FreeCoTaskMem(pv.pszVal);

        var pf = (IPersistFile)link;
        hr = pf.Save(shortcutPath, true);
        if (Failed(hr)) throw new COMException(string.Format("Save failed: 0x{0:X8}", hr), (int)hr);

        Marshal.FinalReleaseComObject(link);
    }
}
"@

[AumidShortcut]::Create($ShortcutPath, $TargetPath, $WorkingDirectory, $IconPath, $AppUserModelID)
Write-Host "Created $ShortcutPath -> $TargetPath (AUMID: $AppUserModelID)"
