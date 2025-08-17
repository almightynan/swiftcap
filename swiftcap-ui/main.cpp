/*
    you may or may not ask: why did i write UI in C++?
    > to piss off the react devs, that's the aim. [https://youtu.be/watch?v=SRgLA8X5N_4]

    TODO: 
    -   dont show elapsed time in tray icon after ending recording
    -   implement screenshot functionality
    -   modularize this code, it's a mess
*/

#include <wx/wx.h>
#include <wx/taskbar.h>
#include <wx/process.h>
#include <signal.h>
#include <wx/button.h>
#include <wx/dcbuffer.h>
#include <wx/timer.h>
#include <wx/filename.h>
#include <wx/stdpaths.h>
#include <wx/display.h>
#include <wx/artprov.h>
#include <wx/textfile.h>

// buttons with modern style and disabled/hover overlay
class ModernButton : public wxButton {
public:
    ModernButton(wxWindow* parent, wxWindowID id, const wxString& label, const wxPoint& pos, const wxSize& size, const wxFont& font)
        : wxButton(parent, id, label, pos, size)
    {
        SetBackgroundColour(wxColour(0x0f,0x0f,0x0f));
        SetForegroundColour(*wxWHITE);
        SetFont(font);
        SetWindowStyleFlag(GetWindowStyleFlag() | wxBORDER_SIMPLE);
        SetMinSize(size);
        SetMaxSize(size);
        SetCursor(wxCursor(wxCURSOR_HAND));
        Bind(wxEVT_ENTER_WINDOW, &ModernButton::OnEnter, this);
        Bind(wxEVT_LEAVE_WINDOW, &ModernButton::OnLeave, this);
        Bind(wxEVT_PAINT, &ModernButton::OnPaint, this);
        Bind(wxEVT_UPDATE_UI, &ModernButton::OnUpdateUI, this);
    }
private:
    bool m_hover = false;
    void OnEnter(wxMouseEvent&) {
        m_hover = true;
        Refresh();
    }
    void OnLeave(wxMouseEvent&) {
        m_hover = false;
        Refresh();
    }
    void OnUpdateUI(wxUpdateUIEvent&) {
        Refresh();
    }
    void OnPaint(wxPaintEvent& evt) {
        wxAutoBufferedPaintDC dc(this);
        wxSize sz = GetSize();
        bool disabled = !IsEnabled();
        wxColour bg = disabled ? wxColour(230, 232, 238) : (m_hover ? wxColour(40,40,44) : wxColour(0x0f,0x0f,0x0f));
        wxColour border = disabled ? wxColour(200,200,210) : *wxWHITE;
        dc.SetPen(wxPen(border, 1));
        dc.SetBrush(bg);
        dc.DrawRoundedRectangle(0, 0, sz.GetWidth(), sz.GetHeight(), 8);
        // draw label
        wxString label = GetLabel();
        dc.SetTextForeground(disabled ? wxColour(180,180,200) : *wxWHITE);
        dc.SetFont(GetFont());
        wxCoord tw, th;
        dc.GetTextExtent(label, &tw, &th);
        dc.DrawText(label, (sz.GetWidth()-tw)/2, (sz.GetHeight()-th)/2);
    }
};

class MyApp : public wxApp
{
public:
    virtual bool OnInit();
};

class MyFrame : public wxFrame
{
public:
    MyFrame(const wxString& title);
    virtual ~MyFrame() {}
private:
    wxProcess* recorderProc = nullptr;
    long recorderPid = 0;
    wxButton* startBtn = nullptr;
    wxButton* stopBtn = nullptr;
    wxMenuItem* trayStartItem = nullptr;
    wxMenuItem* trayStopItem = nullptr;
    wxMenuItem* trayPauseItem = nullptr;
    wxMenuItem* trayResumeItem = nullptr;
    wxMenuItem* trayElapsedItem = nullptr;
    wxTaskBarIcon* trayIcon = nullptr;
    wxMenu* trayMenu = nullptr;
    wxTimer* elapsedTimer = nullptr;
    int elapsedSeconds = 0;
    bool isPaused = false;
    bool flashState = false;
    int playIconTimer = 0;
    int segmentIndex = 0;
    std::vector<wxString> segmentFiles;
    wxString concatListFile;
    void OnStartRecording(wxCommandEvent& event);
    void OnStopRecording(wxCommandEvent& event);
    void OnPauseRecording(wxCommandEvent& event);
    void OnResumeRecording(wxCommandEvent& event);
    void OnRecordingEnded(wxProcessEvent& event);
    void OnElapsedTimer(wxTimerEvent& event);
    void UpdateUIState();
    void UpdateTrayTooltip();
    void UpdateTrayIconWithTime();
    wxDECLARE_EVENT_TABLE();
};

class ToastFrame : public wxFrame {
public:
    ToastFrame(wxWindow* parent, const wxString& filePath);
    ~ToastFrame();
private:
    void OnOpenFolder();
    void OnOpenFile();
    void OnPaint(wxPaintEvent&);
    void OnCloseBtn(wxMouseEvent&);
    void OnTimer(wxTimerEvent&);
    wxString m_filePath;
    wxTimer* m_timer = nullptr;
};

wxIMPLEMENT_APP(MyApp);

bool MyApp::OnInit()
{
    MyFrame *frame = new MyFrame("SwiftCap");
    frame->Show(true);
    return true;
}

MyFrame::MyFrame(const wxString& title)
    : wxFrame(NULL, wxID_ANY, title, wxDefaultPosition, wxSize(480, 220), wxDEFAULT_FRAME_STYLE & ~(wxRESIZE_BORDER | wxMAXIMIZE_BOX))
{
    wxFont modernFont(10, wxFONTFAMILY_MODERN, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_NORMAL);
    wxFont modernFontBold(13, wxFONTFAMILY_MODERN, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_BOLD);
    wxPanel* panel = new wxPanel(this);
    panel->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f));
    panel->Bind(wxEVT_PAINT, [](wxPaintEvent& evt){
        wxPanel* p = static_cast<wxPanel*>(evt.GetEventObject());
        wxPaintDC dc(p);
        wxSize sz = p->GetSize();
        dc.SetPen(wxPen(*wxWHITE, 2));
        dc.SetBrush(*wxTRANSPARENT_BRUSH);
        dc.DrawRoundedRectangle(1, 1, sz.GetWidth()-2, sz.GetHeight()-2, 8);
    });
    wxBoxSizer* mainSizer = new wxBoxSizer(wxVERTICAL);
    wxBoxSizer* topSizer = new wxBoxSizer(wxHORIZONTAL);
    wxStaticText* titleText = new wxStaticText(panel, wxID_ANY, "SwiftCap");
    titleText->SetFont(modernFontBold);
    titleText->SetForegroundColour(*wxWHITE);
    topSizer->Add(titleText, 0, wxLEFT|wxTOP|wxBOTTOM, 18);
    mainSizer->Add(topSizer, 0, wxLEFT|wxRIGHT|wxTOP, 18);
    wxStaticText* desc = new wxStaticText(panel, wxID_ANY, "fast, minimal, cross-platform screen utility tool");
    desc->SetFont(modernFont);
    desc->SetForegroundColour(wxColour(200,200,220));
    mainSizer->Add(desc, 0, wxLEFT|wxRIGHT|wxBOTTOM, 18);
    wxBoxSizer* btnSizer = new wxBoxSizer(wxHORIZONTAL);
    startBtn = new ModernButton(panel, 1003, "Start Recording", wxDefaultPosition, wxSize(150, 40), modernFont);
    stopBtn = new ModernButton(panel, 1004, "Stop Recording", wxDefaultPosition, wxSize(150, 40), modernFont);
    startBtn->Bind(wxEVT_BUTTON, &MyFrame::OnStartRecording, this);
    stopBtn->Bind(wxEVT_BUTTON, &MyFrame::OnStopRecording, this);
    btnSizer->Add(startBtn, 0, wxRIGHT, 18);
    btnSizer->Add(stopBtn, 0);
    mainSizer->Add(btnSizer, 0, wxLEFT|wxRIGHT|wxBOTTOM, 24);
    panel->SetSizer(mainSizer);
    // i chose the current icon because i couldnt find anything else on my pc :^)
    wxIcon icon;
    icon.LoadFile("icon.png", wxBITMAP_TYPE_PNG);
    trayIcon = new wxTaskBarIcon();
    trayMenu = new wxMenu();
    // elapsed time (disabled, updated live)
    int mins = elapsedSeconds / 60;
    int secs = elapsedSeconds % 60;
    wxString elapsedStr = wxString::Format("Elapsed: %02d:%02d", mins, secs);
    trayElapsedItem = trayMenu->Append(wxID_ANY, elapsedStr);
    trayElapsedItem->Enable(false);
    trayMenu->AppendSeparator();
    trayStartItem = trayMenu->Append(1001, "&Start Recording", "Start screen recording");
    trayStopItem = trayMenu->Append(1002, "&Stop Recording", "Stop screen recording");
    trayPauseItem = trayMenu->Append(1003, "&Pause Recording", "Pause recording");
    trayResumeItem = trayMenu->Append(1004, "&Resume Recording", "Resume recording");
    trayMenu->AppendSeparator();
    trayMenu->Append(wxID_EXIT, "&Quit", "Quit the application");

    trayIcon->Bind(wxEVT_TASKBAR_LEFT_DOWN, [this](wxTaskBarIconEvent& event) {
        trayIcon->PopupMenu(trayMenu);
    });
    trayIcon->Bind(wxEVT_MENU, [this](wxCommandEvent& event) { this->OnStartRecording(event); }, 1001);
    trayIcon->Bind(wxEVT_MENU, [this](wxCommandEvent& event) { this->OnStopRecording(event); }, 1002);
    trayIcon->Bind(wxEVT_MENU, [this](wxCommandEvent& event) { this->OnPauseRecording(event); }, 1003);
    trayIcon->Bind(wxEVT_MENU, [this](wxCommandEvent& event) { this->OnResumeRecording(event); }, 1004);
    trayIcon->Bind(wxEVT_MENU, [this](wxCommandEvent& event) {
        trayIcon->RemoveIcon();
        trayIcon->Destroy();
        wxGetApp().ExitMainLoop();
    }, wxID_EXIT);
    trayIcon->SetIcon(icon, "SwiftCap Frontend");
    UpdateUIState();
}

void MyFrame::OnStartRecording(wxCommandEvent& event) {
    if (recorderProc) {
        wxMessageBox("Recording already in progress.", "SwiftCap", wxICON_INFORMATION);
        return;
    }
    // add elapsed item if not present
    if (!trayElapsedItem) {
        int mins = elapsedSeconds / 60;
        int secs = elapsedSeconds % 60;
        wxString elapsedStr = wxString::Format("Elapsed: %02d:%02d", mins, secs);
        trayElapsedItem = new wxMenuItem(trayMenu, wxID_ANY, elapsedStr);
        trayElapsedItem->Enable(false);
        trayMenu->Insert(0, trayElapsedItem);
    }
    segmentIndex = 1;
    segmentFiles.clear();
    // write concat list in current dir with unique name
    concatListFile = wxString::Format("concatlist_%ld.txt", wxGetLocalTimeMillis().GetValue());
    wxFileName segFileName(wxString::Format("out_%d.mp4", segmentIndex));
    segFileName.MakeAbsolute();
    segmentFiles.push_back(segFileName.GetFullPath());
    // get current screen size
    wxDisplay display(wxDisplay::GetFromWindow(this));
    wxRect scr = display.IsOk() ? display.GetClientArea() : wxGetClientDisplayRect();
    int w = scr.GetWidth();
    int h = scr.GetHeight();
    wxString regionArg = wxString::Format("--region %dx%d", w, h);
    wxString cmd = wxString::Format("../swiftcap-go/swiftcap record --out %s --audio on %s", segFileName.GetFullPath(), regionArg);
    recorderProc = new wxProcess(this);
    recorderProc->Redirect();
    recorderPid = wxExecute(cmd, wxEXEC_ASYNC, recorderProc);
    if (recorderPid == 0) {
        wxMessageBox("Failed to start recording.", "SwiftCap", wxICON_ERROR);
        delete recorderProc;
        recorderProc = nullptr;
    } else {
        Bind(wxEVT_END_PROCESS, &MyFrame::OnRecordingEnded, this);
        elapsedSeconds = 0;
        isPaused = false;
        if (!elapsedTimer) {
            elapsedTimer = new wxTimer(this);
            Bind(wxEVT_TIMER, &MyFrame::OnElapsedTimer, this);
        }
        elapsedTimer->Start(1000);
        UpdateTrayIconWithTime();
        wxMessageBox("Recording started!", "SwiftCap", wxICON_INFORMATION);
        UpdateUIState();
    }
}

void MyFrame::OnStopRecording(wxCommandEvent& event) {
    // allow stop if recording or if paused and there are segments
    bool canStop = (recorderProc && recorderPid != 0) || (isPaused && !segmentFiles.empty());
    if (!canStop) {
        wxMessageBox("No recording in progress.", "SwiftCap", wxICON_INFORMATION);
        return;
    }
    if (recorderProc && recorderPid != 0) {
        kill(recorderPid, SIGINT);
        if (elapsedTimer) elapsedTimer->Stop();
        UpdateTrayIconWithTime();
        UpdateUIState();
    }
    // generate output filename: recording_YYYYMMDD_HHMMSS.mp4
    wxDateTime now = wxDateTime::Now();
    wxString outFileName = wxString::Format("recording_%04d%02d%02d_%02d%02d%02d.mp4",
        now.GetYear(), now.GetMonth()+1, now.GetDay(), now.GetHour(), now.GetMinute(), now.GetSecond());
    // concatenate segments
    wxTextFile concatFile(concatListFile);
    concatFile.Create();
    for (const auto& seg : segmentFiles) {
        wxString absSeg = wxFileName(seg).GetFullPath();
        concatFile.AddLine(wxString::Format("file '%s'", absSeg));
    }
    concatFile.Write();
    concatFile.Close();
    wxString concatCmd = wxString::Format("ffmpeg -y -f concat -safe 0 -i %s -c copy %s", concatListFile, outFileName);
    wxExecute(concatCmd, wxEXEC_SYNC);
    // optionally, clean up segment files and concat list
    for (const auto& seg : segmentFiles) wxRemoveFile(seg);
    wxRemoveFile(concatListFile);
    // reset state
    recorderProc = nullptr;
    recorderPid = 0;
    isPaused = false;
    segmentFiles.clear();
    concatListFile = "";
    UpdateTrayIconWithTime();
    UpdateUIState();
    // show toast
    Raise();
    wxFileName outFile(outFileName);
    outFile.MakeAbsolute();
    new ToastFrame(this, outFile.GetFullPath());
}

/*
    the functions OnPauseRecording and OnResumeRecording are used to pause and 
    resume the recording. this is by far the best approach to implement pausing
    and resume recording. if you think you have a better approach, please
    let me know. i will be happy to hear your suggestions.
*/

void MyFrame::OnPauseRecording(wxCommandEvent& event) {
    if (!recorderProc || recorderPid == 0 || isPaused) return;
    kill(recorderPid, SIGINT); // stop current segment
    // do not show toast or finalize, just set paused state
    isPaused = true;
    if (elapsedTimer) elapsedTimer->Stop();
    UpdateTrayIconWithTime();
    UpdateUIState();
}

void MyFrame::OnResumeRecording(wxCommandEvent& event) {
    if (!isPaused) return;
    segmentIndex++;
    wxFileName segFileName(wxString::Format("out_%d.mp4", segmentIndex));
    segFileName.MakeAbsolute();
    segmentFiles.push_back(segFileName.GetFullPath());
    // get current screen size to pass into cli
    wxDisplay display(wxDisplay::GetFromWindow(this));
    wxRect scr = display.IsOk() ? display.GetClientArea() : wxGetClientDisplayRect();
    int w = scr.GetWidth();
    int h = scr.GetHeight();
    wxString regionArg = wxString::Format("--region %dx%d", w, h);
    wxString cmd = wxString::Format("../swiftcap-go/swiftcap record --out %s --audio on %s", segFileName.GetFullPath(), regionArg);
    recorderProc = new wxProcess(this);
    recorderProc->Redirect();
    recorderPid = wxExecute(cmd, wxEXEC_ASYNC, recorderProc);
    isPaused = false;
    if (elapsedTimer) elapsedTimer->Start(1000);
    playIconTimer = 5; // show play icon for 5 seconds, we dont want it to be there all the time
    UpdateTrayIconWithTime();
    UpdateUIState();
}

void MyFrame::OnRecordingEnded(wxProcessEvent& event) {
    if (recorderProc) {
        delete recorderProc;
        recorderProc = nullptr;
    }
    recorderPid = 0;
    if (elapsedTimer && !isPaused) elapsedTimer->Stop();
    UpdateTrayIconWithTime();
    UpdateUIState();
    Unbind(wxEVT_END_PROCESS, &MyFrame::OnRecordingEnded, this);
}

void MyFrame::UpdateUIState() {
    bool recording = (recorderProc != nullptr && recorderPid != 0);
    if (isPaused) {
        if (startBtn) startBtn->Enable(false);
        if (stopBtn) stopBtn->Enable(true);
        if (trayStartItem) trayStartItem->Enable(false);
        if (trayStopItem) trayStopItem->Enable(true);
        if (trayPauseItem) trayPauseItem->Enable(false);
        if (trayResumeItem) trayResumeItem->Enable(true);
    } else {
        if (startBtn) startBtn->Enable(!recording);
        if (stopBtn) stopBtn->Enable(recording);
        if (trayStartItem) trayStartItem->Enable(!recording);
        if (trayStopItem) trayStopItem->Enable(recording);
        if (trayPauseItem) trayPauseItem->Enable(recording);
        if (trayResumeItem) trayResumeItem->Enable(false);
    }
}

void MyFrame::OnElapsedTimer(wxTimerEvent& event) {
    if (!isPaused && recorderProc && recorderPid != 0) {
        ++elapsedSeconds;
    }
    UpdateTrayIconWithTime();
    // update elapsed time in tray menu
    if (trayElapsedItem) {
        int mins = elapsedSeconds / 60;
        int secs = elapsedSeconds % 60;
        wxString elapsedStr = wxString::Format("Elapsed: %02d:%02d", mins, secs);
        trayElapsedItem->SetItemLabel(elapsedStr);
    }
}

void MyFrame::UpdateTrayTooltip() {
    if (!trayIcon) return;
    trayIcon->SetIcon(wxIcon("icon.png", wxBITMAP_TYPE_PNG), "SwiftCap");
}

void MyFrame::UpdateTrayIconWithTime() {
    if (!trayIcon) return;
    wxBitmap baseBmp("icon.png", wxBITMAP_TYPE_PNG);
    if (!baseBmp.IsOk()) {
        trayIcon->SetIcon(wxIcon("icon.png", wxBITMAP_TYPE_PNG), "SwiftCap");
        return;
    }
    bool showOverlay = (recorderProc && recorderPid != 0);
    wxBitmap bmp(baseBmp);
    if (showOverlay) {
        wxMemoryDC dc(bmp);
        int iconW = baseBmp.GetWidth();
        int iconH = baseBmp.GetHeight();
        int overlaySize = iconH / 3;
        int overlayX = iconW - overlaySize - 2;
        int overlayY = iconH - overlaySize - 2;
        if (isPaused) {
            // draw paused icon (two vertical bars) with border to indicate paused state
            dc.SetPen(wxPen(*wxBLACK, 2));
            dc.SetBrush(*wxWHITE_BRUSH);
            int barW = overlaySize / 4;
            int gap = barW;
            dc.DrawRectangle(overlayX + gap/2 - 1, overlayY + 1, barW + 2, overlaySize - 2);
            dc.DrawRectangle(overlayX + barW + gap + gap/2 - 1, overlayY + 1, barW + 2, overlaySize - 2);
            dc.SetPen(*wxWHITE_PEN);
            dc.DrawRectangle(overlayX + gap/2, overlayY + 2, barW, overlaySize - 4);
            dc.DrawRectangle(overlayX + barW + gap + gap/2, overlayY + 2, barW, overlaySize - 4);
        } else if (playIconTimer > 0) {
            // draw play icon (triangle) with border to indicate resume state
            wxPoint pts[3] = {
                wxPoint(overlayX + 3, overlayY + 2),
                wxPoint(overlayX + overlaySize - 3, overlayY + overlaySize/2),
                wxPoint(overlayX + 3, overlayY + overlaySize - 2)
            };
            dc.SetPen(wxPen(*wxBLACK, 2));
            dc.SetBrush(*wxWHITE_BRUSH);
            dc.DrawPolygon(3, pts);
            dc.SetPen(*wxWHITE_PEN);
            dc.SetBrush(*wxWHITE_BRUSH);
            dc.DrawPolygon(3, pts);
        } else {
            // draw flashing red dot with border to indicate recording state
            if (flashState) {
                dc.SetPen(wxPen(*wxBLACK, 2));
                dc.SetBrush(wxBrush(wxColour(220,40,40)));
                dc.DrawCircle(overlayX + overlaySize/2, overlayY + overlaySize/2, overlaySize/3 + 1);
                dc.SetPen(*wxTRANSPARENT_PEN);
                dc.SetBrush(wxBrush(wxColour(220,40,40)));
                dc.DrawCircle(overlayX + overlaySize/2, overlayY + overlaySize/2, overlaySize/3 - 1);
            }
        }
        dc.SelectObject(wxNullBitmap);
    }
    wxIcon icon;
    icon.CopyFromBitmap(bmp);
    trayIcon->SetIcon(icon, "SwiftCap");
    // timer logic for flashing and play icon
    if (showOverlay) {
        if (!isPaused && playIconTimer > 0) {
            playIconTimer--;
        } else if (!isPaused) {
            flashState = !flashState;
        }
    } else {
        playIconTimer = 0;
        flashState = false;
    }
}

wxBEGIN_EVENT_TABLE(MyFrame, wxFrame)
    EVT_END_PROCESS(wxID_ANY, MyFrame::OnRecordingEnded)
wxEND_EVENT_TABLE()

ToastFrame::ToastFrame(wxWindow* parent, const wxString& filePath)
    : wxFrame(parent, wxID_ANY, "", wxDefaultPosition, wxDefaultSize, wxFRAME_NO_TASKBAR | wxSTAY_ON_TOP | wxBORDER_NONE), m_filePath(filePath)
{
    wxFont modernFont(9, wxFONTFAMILY_MODERN, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_NORMAL);
    wxFont modernFontBold(12, wxFONTFAMILY_MODERN, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_BOLD);
    wxPanel* bgPanel = new wxPanel(this, wxID_ANY);
    bgPanel->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f,255)); // #0f0f0f
    SetTransparent(255);
    bgPanel->SetMinSize(wxSize(420, 1));
    bgPanel->Bind(wxEVT_PAINT, &ToastFrame::OnPaint, this);
    wxBoxSizer* outerSizer = new wxBoxSizer(wxVERTICAL);
    wxBoxSizer* topSizer = new wxBoxSizer(wxHORIZONTAL);
    // X button: custom panel with smaller, modern white X
    wxPanel* closePanel = new wxPanel(bgPanel, wxID_ANY, wxDefaultPosition, wxSize(24,24));
    closePanel->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f));
    closePanel->Bind(wxEVT_PAINT, [](wxPaintEvent& evt){
        wxPaintDC dc(static_cast<wxWindow*>(evt.GetEventObject()));
        dc.SetPen(wxPen(*wxWHITE, 2, wxPENSTYLE_SOLID));
        wxSize sz = dc.GetSize();
        int pad = 7;
        dc.DrawLine(pad, pad, sz.GetWidth()-pad, sz.GetHeight()-pad);
        dc.DrawLine(sz.GetWidth()-pad, pad, pad, sz.GetHeight()-pad);
    });
    closePanel->SetCursor(wxCursor(wxCURSOR_HAND));
    closePanel->Bind(wxEVT_LEFT_DOWN, [this](wxMouseEvent&){ this->Destroy(); });
    closePanel->Bind(wxEVT_ENTER_WINDOW, [closePanel](wxMouseEvent&){ closePanel->SetBackgroundColour(wxColour(30,30,30)); closePanel->Refresh(); });
    closePanel->Bind(wxEVT_LEAVE_WINDOW, [closePanel](wxMouseEvent&){ closePanel->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f)); closePanel->Refresh(); });
    topSizer->AddStretchSpacer();
    topSizer->Add(closePanel, 0, wxTOP|wxRIGHT|wxLEFT, 4);
    outerSizer->Add(topSizer, 0, wxEXPAND|wxLEFT|wxRIGHT, 12);
    wxBoxSizer* sizer = new wxBoxSizer(wxVERTICAL);
    wxFileName absFile(filePath);
    wxString absPath = absFile.GetFullPath();
    // success icon + 'Recording saved:'
#if defined(wxART_TICK_MARK)
    wxBoxSizer* msgRow = new wxBoxSizer(wxHORIZONTAL);
    wxStaticBitmap* icon = new wxStaticBitmap(bgPanel, wxID_ANY, wxArtProvider::GetBitmap(wxART_TICK_MARK, wxART_OTHER, wxSize(18,18)));
    msgRow->Add(icon, 0, wxALIGN_CENTER_VERTICAL|wxRIGHT, 6);
    wxStaticText* msg = new wxStaticText(bgPanel, wxID_ANY, "Recording saved:");
    msg->SetForegroundColour(*wxWHITE);
    msg->SetFont(modernFontBold);
    msgRow->Add(msg, 0, wxALIGN_CENTER_VERTICAL);
    sizer->Add(msgRow, 0, wxLEFT|wxRIGHT|wxTOP, 20);
#else
    wxStaticText* msg = new wxStaticText(bgPanel, wxID_ANY, "Recording saved:");
    msg->SetForegroundColour(*wxWHITE);
    msg->SetFont(modernFontBold);
    sizer->Add(msg, 0, wxLEFT|wxRIGHT|wxTOP, 20);
#endif
    wxStaticText* pathText = new wxStaticText(bgPanel, wxID_ANY, absPath);
    pathText->SetForegroundColour(wxColour(200,200,255));
    pathText->SetFont(modernFont);
    sizer->Add(pathText, 0, wxLEFT|wxRIGHT|wxBOTTOM, 18);
    wxBoxSizer* btnSizer = new wxBoxSizer(wxHORIZONTAL);
    auto makeBtn = [bgPanel, modernFont](const wxString& label, std::function<void(wxCommandEvent&)> onClick) {
        wxButton* btn = new wxButton(bgPanel, wxID_ANY, label, wxDefaultPosition, wxSize(110,32));
        btn->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f));
        btn->SetForegroundColour(*wxWHITE);
        btn->SetFont(modernFont);
        btn->SetCursor(wxCursor(wxCURSOR_HAND));
        btn->SetWindowStyleFlag(btn->GetWindowStyleFlag() | wxBORDER_SIMPLE);
        btn->SetMinSize(wxSize(110,32));
        btn->SetMaxSize(wxSize(110,32));
        btn->Bind(wxEVT_BUTTON, onClick);
        btn->Bind(wxEVT_ENTER_WINDOW, [btn](wxMouseEvent&){ btn->SetBackgroundColour(wxColour(40,40,44)); btn->Refresh(); });
        btn->Bind(wxEVT_LEAVE_WINDOW, [btn](wxMouseEvent&){ btn->SetBackgroundColour(wxColour(0x0f,0x0f,0x0f)); btn->Refresh(); });
        return btn;
    };
    btnSizer->Add(makeBtn("Open Folder", [this](wxCommandEvent&){ OnOpenFolder(); }), 0, wxRIGHT, 14);
    btnSizer->Add(makeBtn("Open File", [this](wxCommandEvent&){ OnOpenFile(); }), 0);
    sizer->Add(btnSizer, 0, wxALIGN_RIGHT | wxLEFT|wxRIGHT|wxBOTTOM, 20);
    outerSizer->Add(sizer, 0, wxEXPAND);
    bgPanel->SetSizer(outerSizer);
    bgPanel->SetMinSize(wxSize(420, 1));
    bgPanel->Layout();
    Fit();
    SetMinSize(wxSize(420, bgPanel->GetBestSize().GetHeight()+20));
#if wxUSE_DISPLAY
    wxDisplay display(wxDisplay::GetFromWindow(parent));
    wxRect scr = display.IsOk() ? display.GetClientArea() : wxGetClientDisplayRect();
#else
    int w, h;
    wxGetDisplaySize(&w, &h);
    wxRect scr(0, 0, w, h);
#endif
    Layout();
    wxSize frameSz = GetSize();
    Move(scr.GetRight() - frameSz.GetWidth(), scr.GetBottom() - frameSz.GetHeight());
    Show();
    // auto-close after 7 seconds
    m_timer = new wxTimer(this);
    Bind(wxEVT_TIMER, &ToastFrame::OnTimer, this);
    m_timer->StartOnce(7000);
}

ToastFrame::~ToastFrame() {
    if (m_timer) {
        m_timer->Stop();
        Unbind(wxEVT_TIMER, &ToastFrame::OnTimer, this);
        delete m_timer;
        m_timer = nullptr;
    }
}

void ToastFrame::OnCloseBtn(wxMouseEvent&) { Close(); }

void ToastFrame::OnPaint(wxPaintEvent& evt) {
    wxPanel* panel = static_cast<wxPanel*>(evt.GetEventObject());
    wxPaintDC dc(panel);
    wxSize sz = panel->GetSize();
    dc.SetPen(wxPen(*wxWHITE, 2));
    dc.SetBrush(*wxTRANSPARENT_BRUSH);
    dc.DrawRoundedRectangle(1, 1, sz.GetWidth()-2, sz.GetHeight()-2, 2);
}

void ToastFrame::OnOpenFolder() {
    wxFileName fn(m_filePath);
    fn.MakeAbsolute();
    wxString folder = fn.GetPath();
    if (folder.IsEmpty() || !wxDirExists(folder)) {
        wxMessageBox("Folder does not exist or path is empty:\n" + folder, "Error", wxICON_ERROR);
        return;
    }
    wxLaunchDefaultApplication(folder);
}
void ToastFrame::OnOpenFile() {
    wxFileName fn(m_filePath);
    fn.MakeAbsolute();
    wxString absFile = fn.GetFullPath();
    if (!wxFileExists(absFile)) {
        wxMessageBox("File does not exist:\n" + absFile, "Error", wxICON_ERROR);
        return;
    }
    wxLaunchDefaultApplication(absFile);
}
void ToastFrame::OnTimer(wxTimerEvent&) {
    Destroy();
}