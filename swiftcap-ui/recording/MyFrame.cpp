#include <wx/app.h>
#include "../recording/MyFrame.h"
#include <wx/filename.h>
#include <wx/stdpaths.h>
#include <wx/display.h>
#include <wx/artprov.h>
#include <wx/textfile.h>
#include <signal.h>
#include <wx/dcbuffer.h>
#include <memory>

class CountdownOverlay : public wxFrame {
public:
    CountdownOverlay(MyFrame* parent)
        : wxFrame(nullptr, wxID_ANY, "", wxDefaultPosition, wxDefaultSize,
                  wxFRAME_NO_TASKBAR | wxSTAY_ON_TOP | wxFRAME_SHAPED | wxBORDER_NONE),
          m_parent(parent), m_count(3), m_cancelled(false) {
        SetBackgroundStyle(wxBG_STYLE_PAINT);
        SetTransparent(200);
        wxRect scr = wxDisplay((unsigned int)0).GetGeometry();
        SetSize(scr);
        Move(scr.GetTopLeft());
        SetWindowStyleFlag(wxFRAME_NO_TASKBAR | wxSTAY_ON_TOP | wxFRAME_SHAPED | wxBORDER_NONE);
        Raise();
        ShowFullScreen(true, wxFULLSCREEN_ALL);
        SetFocus();
        AcceptsFocus();
        Bind(wxEVT_LEFT_DOWN, &CountdownOverlay::OnCancelByClick, this);
        Bind(wxEVT_RIGHT_DOWN, &CountdownOverlay::OnCancelByClick, this);
        Bind(wxEVT_MIDDLE_DOWN, &CountdownOverlay::OnCancelByClick, this);
        Bind(wxEVT_PAINT, &CountdownOverlay::OnPaint, this);
        m_timer.Bind(wxEVT_TIMER, &CountdownOverlay::OnTimer, this);
        m_timer.Start(1000);
    }
    void OnCancelByClick(wxMouseEvent&) {
        m_cancelled = true;
        m_timer.Stop();
        Hide();
        if (m_parent) m_parent->CancelPendingRecording();
        Destroy();
    }
    void OnPaint(wxPaintEvent&) {
        wxAutoBufferedPaintDC dc(this);
        dc.SetBrush(wxBrush(wxColour(0,0,0,200)));
        dc.SetPen(*wxTRANSPARENT_PEN);
        dc.DrawRectangle(GetClientRect());
        wxFont font(80, wxFONTFAMILY_SWISS, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_BOLD);
        dc.SetFont(font);
        dc.SetTextForeground(*wxWHITE);
        wxString text = wxString::Format("%d", m_count);
        wxSize sz = dc.GetTextExtent(text);
        wxSize winSz = GetClientSize();
        dc.DrawText(text, (winSz.x-sz.x)/2, (winSz.y-sz.y)/2);
        int blockHeight = 50; // make this dynamic based on taskbar height
        dc.SetBrush(wxBrush(wxColour(0,0,0)));
        // dc.SetPen(*wxTRANSPARENT_PEN);
        dc.DrawRectangle(0, winSz.y-blockHeight, winSz.x, blockHeight);
        wxFont msgFont(18, wxFONTFAMILY_SWISS, wxFONTSTYLE_NORMAL, wxFONTWEIGHT_NORMAL);
        dc.SetFont(msgFont);
        dc.SetTextForeground(wxColour(220,220,220));
        wxString msg = "Click anywhere to cancel this countdown and abort recording";
        wxSize msgSz = dc.GetTextExtent(msg);
        dc.DrawText(msg, (winSz.x-msgSz.x)/2, winSz.y-blockHeight+(blockHeight-msgSz.y)/2);
    }
    void OnTimer(wxTimerEvent&) {
        m_count--;
        if (m_count == 0) {
            m_timer.Stop();
            Hide();
            if (!m_cancelled && m_parent) m_parent->StartActualRecording();
            Destroy();
        } else {
            Refresh();
        }
    }
private:
    MyFrame* m_parent;
    bool m_cancelled = false;
    wxTimer m_timer;
    int m_count;
};


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
    wxIcon icon;
    icon.LoadFile("icon.png", wxBITMAP_TYPE_PNG);
    trayIcon = new wxTaskBarIcon();
    trayMenu = new wxMenu();
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
    wxTheApp->ExitMainLoop();
}, wxID_EXIT);
    trayIcon->SetIcon(icon, "SwiftCap Frontend");
    UpdateUIState();
}


void MyFrame::OnStartRecording(wxCommandEvent& event) {
    if (recorderProc) {
        wxMessageBox("Recording already in progress.", "SwiftCap", wxICON_INFORMATION);
        return;
    }
    if (!trayElapsedItem) {
        int mins = elapsedSeconds / 60;
        int secs = elapsedSeconds % 60;
        wxString elapsedStr = wxString::Format("Elapsed: %02d:%02d", mins, secs);
        trayElapsedItem = new wxMenuItem(trayMenu, wxID_ANY, elapsedStr);
        trayElapsedItem->Enable(false);
        trayMenu->Insert(0, trayElapsedItem);
    }
    new CountdownOverlay(this);
    return;
}

void MyFrame::StartActualRecording() {
    segmentIndex = 1;
    segmentFiles.clear();
    wxString videosDir = wxStandardPaths::Get().GetUserDir(wxStandardPaths::Dir_Videos);
    if (!wxDirExists(videosDir)) {
        wxMkdir(videosDir);
    }
    concatListFile = wxFileName(videosDir, wxString::Format("concatlist_%ld.txt", wxGetLocalTimeMillis().GetValue())).GetFullPath();
    wxFileName segFileName(videosDir, wxString::Format("out_%d.mp4", segmentIndex));
    segFileName.MakeAbsolute();
    segmentFiles.push_back(segFileName.GetFullPath());
    wxDisplay display(wxDisplay::GetFromWindow(this));
    wxRect scr = display.IsOk() ? display.GetGeometry() : wxGetClientDisplayRect();
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
        UpdateUIState();
    }
}

void MyFrame::OnStopRecording(wxCommandEvent& event) {
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
    wxDateTime now = wxDateTime::Now();
    wxString videosDir = wxStandardPaths::Get().GetUserDir(wxStandardPaths::Dir_Videos);
    if (!wxDirExists(videosDir)) {
        wxMkdir(videosDir);
    }
    wxString outFileName = wxFileName(videosDir, wxString::Format("recording_%04d%02d%02d_%02d%02d%02d.mp4",
        now.GetYear(), now.GetMonth()+1, now.GetDay(), now.GetHour(), now.GetMinute(), now.GetSecond())).GetFullPath();
    if (!concatListFile.IsEmpty()) {
        wxString missingSegs;
        for (const auto& seg : segmentFiles) {
            if (!wxFileExists(seg)) {
                missingSegs += seg + "\n";
            }
        }
        if (!missingSegs.IsEmpty()) {
            wxMessageBox("Missing segment files:\n" + missingSegs, "SwiftCap Debug", wxICON_ERROR);
        }
        if (concatListFile.IsEmpty()) {
            wxMessageBox("Concat list file path is empty!", "SwiftCap Debug", wxICON_ERROR);
            return;
        }
        wxTextFile concatFile(concatListFile);
        if (!wxFileExists(concatListFile)) {
            concatFile.Create();
        }
        for (const auto& seg : segmentFiles) {
            wxString absSeg = wxFileName(seg).GetFullPath();
            concatFile.AddLine(wxString::Format("file '%s'", absSeg));
        }
        concatFile.Write();
        concatFile.Close();
        wxString concatCmd = wxString::Format("ffmpeg -y -loglevel error -f concat -safe 0 -i %s -c copy %s", concatListFile, outFileName);
        wxArrayString ffmpegOutput, ffmpegErrors;
        long ffmpegRet = wxExecute(concatCmd, ffmpegOutput, ffmpegErrors, wxEXEC_SYNC);
        if (!wxFileExists(outFileName)) {
            wxString errMsg = "ffmpeg failed to create output file!\n";
            errMsg += "Command: " + concatCmd + "\n";
            errMsg += "Stdout:\n";
            for (auto& l : ffmpegOutput) errMsg += l + "\n";
            errMsg += "Stderr:\n";
            for (auto& l : ffmpegErrors) errMsg += l + "\n";
            wxMessageBox(errMsg, "SwiftCap Error", wxICON_ERROR | wxOK);
        }
        for (const auto& seg : segmentFiles) {
            if (wxFileExists(seg)) wxRemoveFile(seg);
        }
        if (wxFileExists(concatListFile)) wxRemoveFile(concatListFile);
    }
    recorderProc = nullptr;
    recorderPid = 0;
    isPaused = false;
    segmentFiles.clear();
    concatListFile = "";
    UpdateTrayIconWithTime();
    UpdateUIState();
    Raise();
    wxFileName outFile(outFileName);
    outFile.MakeAbsolute();
    new ToastFrame(this, outFile.GetFullPath());
}

void MyFrame::OnPauseRecording(wxCommandEvent& event) {
    if (!recorderProc || recorderPid == 0 || isPaused) return;
    kill(recorderPid, SIGINT);
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
    wxDisplay display(wxDisplay::GetFromWindow(this));
    wxRect scr = display.IsOk() ? display.GetGeometry() : wxGetClientDisplayRect();
    int w = scr.GetWidth();
    int h = scr.GetHeight();
    wxString regionArg = wxString::Format("--region %dx%d", w, h);
    wxString cmd = wxString::Format("../swiftcap-go/swiftcap record --out %s --audio on %s", segFileName.GetFullPath(), regionArg);
    recorderProc = new wxProcess(this);
    recorderProc->Redirect();
    recorderPid = wxExecute(cmd, wxEXEC_ASYNC, recorderProc);
    isPaused = false;
    if (elapsedTimer) elapsedTimer->Start(1000);
    playIconTimer = 5;
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

void MyFrame::CancelPendingRecording() {
    // reset UI state to idle
    if (elapsedTimer) {
        elapsedTimer->Stop();
    }
    isPaused = false;
    recorderProc = nullptr;
    recorderPid = 0;
    segmentFiles.clear();
    concatListFile = "";
    UpdateTrayIconWithTime();
    UpdateUIState();
}

wxBEGIN_EVENT_TABLE(MyFrame, wxFrame)
    EVT_END_PROCESS(wxID_ANY, MyFrame::OnRecordingEnded)
wxEND_EVENT_TABLE()
