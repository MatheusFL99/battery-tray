// Instalador gráfico da Bateria dos Dispositivos (WinForms, sem dependências
// além do .NET Framework que já vem no Windows).
//
// Compilado pelo build.ps1 com o csc do próprio Windows:
//   C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe
//
// Uso:
//   Setup.exe                  -> instalação gráfica
//   Setup.exe /install         -> instalação silenciosa (padrão, com autostart)
//   Setup.exe /uninstall       -> desinstalação silenciosa
// Código de saída: 0 = ok, 1 = erro (detalhes em %TEMP%\batterytray-setup.log).
//
// Observação: o código é C# 5 (compatível com o csc do .NET 4.0) — sem
// interpolação de string, sem `?.`, sem membros expression-bodied.
using System;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Reflection;
using System.Text;
using System.Windows.Forms;

namespace BatteryTraySetup
{
    static class Program
    {
        [STAThread]
        static int Main(string[] args)
        {
            string mode = args.Length > 0 ? args[0].Trim().ToLowerInvariant() : "";

            if (mode == "/install" || mode == "/instalar")
                return Installer.Run(install: true, log: true);

            if (mode == "/uninstall" || mode == "/desinstalar")
                return Installer.Run(install: false, log: true);

            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Application.Run(new SetupForm());
            return 0;
        }
    }

    /// <summary>Constantes e operações de instalação (usadas pela UI e pelo modo silencioso).</summary>
    static class Installer
    {
        public const string AppName = "Bateria dos Dispositivos";
        public const string ExeName = "batterytray.exe";
        public const string StartupLinkName = "Bateria Dispositivos.lnk";

        public static readonly string DefaultDir =
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "BatteryTray");
        public static readonly string StartupDir =
            Environment.GetFolderPath(Environment.SpecialFolder.Startup);
        public static readonly string ProgramsDir =
            Environment.GetFolderPath(Environment.SpecialFolder.Programs);

        private const string RegKeyPath = @"Software\BatteryTray";

        // ----- estado -----

        /// <summary>Pasta onde o app está (ou ficará) instalado.</summary>
        public static string CurrentInstallDir
        {
            get
            {
                try
                {
                    using (var k = Microsoft.Win32.Registry.CurrentUser.OpenSubKey(RegKeyPath))
                    {
                        if (k != null)
                        {
                            object v = k.GetValue("InstallDir");
                            if (v is string && Directory.Exists((string)v))
                                return (string)v;
                        }
                    }
                }
                catch { /* sem registro = usa o padrão */ }
                return DefaultDir;
            }
        }

        public static bool IsInstalled
        {
            get { return File.Exists(Path.Combine(CurrentInstallDir, ExeName)); }
        }

        public static bool AutostartEnabled
        {
            get { return File.Exists(Path.Combine(StartupDir, StartupLinkName)); }
        }

        // ----- silencioso -----

        public static int Run(bool install, bool log)
        {
            try
            {
                if (install)
                    Install(DefaultDir, true, false);
                else
                    Uninstall();
                if (log) WriteLog(install ? "instalado" : "desinstalado", null);
                return 0;
            }
            catch (Exception ex)
            {
                if (log) WriteLog("erro", ex);
                return 1;
            }
        }

        private static void WriteLog(string msg, Exception ex)
        {
            try
            {
                string p = Path.Combine(Path.GetTempPath(), "batterytray-setup.log");
                var sb = new StringBuilder();
                sb.AppendLine(DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss") + " -> " + msg);
                if (ex != null) sb.AppendLine(ex.ToString());
                File.AppendAllText(p, sb.ToString());
            }
            catch { /* log é melhor esforço */ }
        }

        // ----- operações -----

        /// <summary>Instala o app na pasta indicada.</summary>
        /// <param name="dir">pasta de destino</param>
        /// <param name="autostart">criar atalho na Startup (iniciar com o Windows)</param>
        /// <param name="launch">executar o app depois de instalar</param>
        public static void Install(string dir, bool autostart, bool launch)
        {
            if (string.IsNullOrEmpty(dir))
                throw new InvalidOperationException("Pasta de instalação vazia.");

            byte[] payload = LoadEmbeddedApp(); // o app vem dentro do Setup.exe
            string src = null;
            if (payload == null)
            {
                // fallback: batterytray.exe ao lado do Setup.exe (build local)
                src = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, ExeName);
                if (!File.Exists(src))
                    throw new FileNotFoundException(
                        "Não achei " + ExeName + " embutido no Setup.exe nem ao lado dele.");
            }

            KillApp();
            Directory.CreateDirectory(dir);
            string dest = Path.Combine(dir, ExeName);
            if (payload != null)
                File.WriteAllBytes(dest, payload);
            else
                File.Copy(src, dest, true);

            // marca a localização para a desinstalação futura
            using (var k = Microsoft.Win32.Registry.CurrentUser.CreateSubKey(RegKeyPath))
            {
                if (k != null) k.SetValue("InstallDir", dir);
            }

            string target = Path.Combine(dir, ExeName);
            CreateShortcut(Path.Combine(ProgramsDir, AppName, AppName + ".lnk"), target, 1);
            if (autostart)
                CreateShortcut(Path.Combine(StartupDir, StartupLinkName), target, 1);
            else
                DeleteIfExists(Path.Combine(StartupDir, StartupLinkName));

            if (launch) StartApp(target);
        }

        /// <summary>Desinstala: encerra o app, remove atalhos e a pasta instalada.</summary>
        public static void Uninstall()
        {
            KillApp();
            DeleteIfExists(Path.Combine(StartupDir, StartupLinkName));

            string smFolder = Path.Combine(ProgramsDir, AppName);
            try { if (Directory.Exists(smFolder)) Directory.Delete(smFolder, true); }
            catch { /* pode estar em uso momentâneo */ }

            string dir = CurrentInstallDir;
            try
            {
                if (Directory.Exists(dir))
                {
                    // dá uma chance ao Windows liberar os arquivos
                    for (int i = 0; i < 3 && Directory.Exists(dir); i++)
                    {
                        try { Directory.Delete(dir, true); }
                        catch { System.Threading.Thread.Sleep(300); }
                    }
                }
            }
            catch { /* a pasta é melhor esforço */ }

            try { Microsoft.Win32.Registry.CurrentUser.DeleteSubKeyTree(RegKeyPath, false); }
            catch { /* sem chave = nada a limpar */ }
        }

        /// <summary>Lê o batterytray.exe embutido como recurso no próprio Setup.</summary>
        private static byte[] LoadEmbeddedApp()
        {
            try
            {
                Assembly asm = Assembly.GetExecutingAssembly();
                using (Stream s = asm.GetManifestResourceStream("BatteryTraySetup.app"))
                {
                    if (s == null) return null;
                    using (var ms = new MemoryStream())
                    {
                        s.CopyTo(ms); // .NET 4.0 tem Stream.CopyTo ✓
                        return ms.ToArray();
                    }
                }
            }
            catch
            {
                return null; // deixa o fallback procurar ao lado do Setup.exe
            }
        }

        public static void StartApp(string exe)
        {
            if (!File.Exists(exe)) return;
            var psi = new ProcessStartInfo(exe);
            psi.UseShellExecute = true;
            psi.WorkingDirectory = Path.GetDirectoryName(exe);
            try { Process.Start(psi); }
            catch { /* o usuário pode abrir pelo atalho */ }
        }

        private static void KillApp()
        {
            foreach (var p in Process.GetProcessesByName("batterytray"))
            {
                try
                {
                    p.Kill();
                    p.WaitForExit(3000);
                }
                catch { /* processo pode já ter saído */ }
            }
        }

        /// <summary>Cria (ou recria) um atalho .lnk via WScript.Shell (reflexão,
        /// para não depender de `dynamic`/Microsoft.CSharp no csc 4.0).</summary>
        public static void CreateShortcut(string lnkPath, string target, int windowStyle)
        {
            string dir = Path.GetDirectoryName(lnkPath);
            if (!string.IsNullOrEmpty(dir)) Directory.CreateDirectory(dir);

            Type t = Type.GetTypeFromProgID("WScript.Shell");
            if (t == null) throw new InvalidOperationException("WScript.Shell indisponível.");

            object sh = Activator.CreateInstance(t);
            object lnk = null;
            try
            {
                lnk = t.InvokeMember("CreateShortcut", BindingFlags.InvokeMethod,
                    null, sh, new object[] { lnkPath });
                Type lt = lnk.GetType();
                lt.InvokeMember("TargetPath", BindingFlags.SetProperty, null, lnk, new object[] { target });
                lt.InvokeMember("WorkingDirectory", BindingFlags.SetProperty, null, lnk,
                    new object[] { Path.GetDirectoryName(target) });
                lt.InvokeMember("WindowStyle", BindingFlags.SetProperty, null, lnk, new object[] { windowStyle });
                lt.InvokeMember("Description", BindingFlags.SetProperty, null, lnk, new object[] { AppName });
                lt.InvokeMember("Save", BindingFlags.InvokeMethod, null, lnk, null);
            }
            finally
            {
                if (lnk != null)
                {
                    try { System.Runtime.InteropServices.Marshal.ReleaseComObject(lnk); }
                    catch { /* já liberado */ }
                }
                try { System.Runtime.InteropServices.Marshal.ReleaseComObject(sh); }
                catch { /* já liberado */ }
            }
        }

        private static void DeleteIfExists(string path)
        {
            try { if (File.Exists(path)) File.Delete(path); }
            catch { /* já não estava lá */ }
        }
    }

    class SetupForm : Form
    {
        private TextBox dirBox;
        private CheckBox autostartBox;
        private CheckBox runBox;
        private Label statusLabel;
        private Button installBtn;
        private Button uninstallBtn;

        public SetupForm()
        {
            BuildUi();
            RefreshState();
        }

        private void BuildUi()
        {
            Text = "Bateria dos Dispositivos — Instalação";
            ClientSize = new Size(470, 300);
            FormBorderStyle = FormBorderStyle.FixedDialog;
            MaximizeBox = false;
            MinimizeBox = false;
            StartPosition = FormStartPosition.CenterScreen;
            Font = new Font("Segoe UI", 9F);

            try
            {
                string ico = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "icon.ico");
                if (File.Exists(ico)) Icon = Icon.ExtractAssociatedIcon(ico);
            }
            catch { /* ícone é cosmético */ }

            var title = new Label();
            title.Text = "Instalar Bateria dos Dispositivos";
            title.Font = new Font(Font, FontStyle.Bold);
            title.Font = new Font(title.Font.FontFamily, 13F, FontStyle.Bold);
            title.AutoSize = true;
            title.Location = new Point(22, 16);
            Controls.Add(title);

            var hint = new Label();
            hint.Text = "Sem M HUB nem G HUB: lê a bateria dos periféricos direto por HID,\n" +
                        "com notificação de bateria baixa (10% e 5%).";
            hint.AutoSize = true;
            hint.Location = new Point(24, 50);
            hint.ForeColor = Color.FromArgb(60, 60, 60);
            Controls.Add(hint);

            var dirLabel = new Label();
            dirLabel.Text = "Pasta de instalação:";
            dirLabel.AutoSize = true;
            dirLabel.Location = new Point(24, 96);
            Controls.Add(dirLabel);

            dirBox = new TextBox();
            dirBox.Location = new Point(24, 116);
            dirBox.Size = new Size(330, 24);
            Controls.Add(dirBox);

            var browse = new Button();
            browse.Text = "Procurar…";
            browse.Location = new Point(360, 114);
            browse.Size = new Size(86, 28);
            browse.Click += delegate
            {
                using (var dlg = new FolderBrowserDialog())
                {
                    dlg.Description = "Escolha onde instalar";
                    dlg.SelectedPath = dirBox.Text;
                    if (dlg.ShowDialog(this) == DialogResult.OK)
                        dirBox.Text = dlg.SelectedPath;
                }
            };
            Controls.Add(browse);

            autostartBox = new CheckBox();
            autostartBox.Text = "Iniciar automaticamente com o Windows";
            autostartBox.AutoSize = true;
            autostartBox.Location = new Point(24, 152);
            autostartBox.Checked = true;
            Controls.Add(autostartBox);

            runBox = new CheckBox();
            runBox.Text = "Executar agora";
            runBox.AutoSize = true;
            runBox.Location = new Point(24, 178);
            runBox.Checked = true;
            Controls.Add(runBox);

            installBtn = new Button();
            installBtn.Text = "Instalar";
            installBtn.Size = new Size(130, 34);
            installBtn.Location = new Point(24, 214);
            installBtn.Font = new Font(Font, FontStyle.Bold);
            installBtn.Click += OnInstall;
            Controls.Add(installBtn);

            uninstallBtn = new Button();
            uninstallBtn.Text = "Desinstalar";
            uninstallBtn.Size = new Size(130, 34);
            uninstallBtn.Location = new Point(162, 214);
            uninstallBtn.Click += OnUninstall;
            Controls.Add(uninstallBtn);

            var closeBtn = new Button();
            closeBtn.Text = "Fechar";
            closeBtn.Size = new Size(100, 34);
            closeBtn.Location = new Point(346, 214);
            closeBtn.Click += delegate { Close(); };
            Controls.Add(closeBtn);

            statusLabel = new Label();
            statusLabel.Location = new Point(24, 258);
            statusLabel.Size = new Size(422, 34);
            statusLabel.ForeColor = Color.FromArgb(30, 110, 40);
            Controls.Add(statusLabel);
        }

        private void RefreshState()
        {
            if (Installer.IsInstalled)
            {
                dirBox.Text = Installer.CurrentInstallDir;
                statusLabel.Text = "Instalado em " + Installer.CurrentInstallDir;
            }
            else
            {
                dirBox.Text = Installer.DefaultDir;
                statusLabel.Text = "Não instalado ainda.";
            }
            autostartBox.Checked = Installer.AutostartEnabled || !Installer.IsInstalled;
        }

        private void OnInstall(object sender, EventArgs e)
        {
            try
            {
                Installer.Install(dirBox.Text.Trim(), autostartBox.Checked, runBox.Checked);
                statusLabel.ForeColor = Color.FromArgb(30, 110, 40);
                statusLabel.Text = "Instalado com sucesso em " + dirBox.Text.Trim();
                RefreshState();
            }
            catch (Exception ex)
            {
                statusLabel.ForeColor = Color.FromArgb(170, 40, 40);
                statusLabel.Text = "Erro: " + ex.Message;
                MessageBox.Show(this, ex.Message, Installer.AppName,
                    MessageBoxButtons.OK, MessageBoxIcon.Error);
            }
        }

        private void OnUninstall(object sender, EventArgs e)
        {
            var confirm = MessageBox.Show(this,
                "Remover a Bateria dos Dispositivos desta máquina?",
                Installer.AppName, MessageBoxButtons.YesNo, MessageBoxIcon.Question);
            if (confirm != DialogResult.Yes) return;

            try
            {
                Installer.Uninstall();
                statusLabel.ForeColor = Color.FromArgb(60, 60, 60);
                statusLabel.Text = "Desinstalado.";
                dirBox.Text = Installer.DefaultDir;
                autostartBox.Checked = true;
            }
            catch (Exception ex)
            {
                statusLabel.ForeColor = Color.FromArgb(170, 40, 40);
                statusLabel.Text = "Erro: " + ex.Message;
            }
        }
    }
}
