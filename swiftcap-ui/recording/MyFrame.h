
// recording/MyFrame.h
#pragma once
#include <wx/wx.h>
#include <wx/taskbar.h>
#include <wx/process.h>
#include <wx/timer.h>
#include <vector>
#include "../ui/ModernButton.h"
#include "../ui/ToastFrame.h"

class MyFrame : public wxFrame {
public:
    MyFrame(const wxString& title);
    virtual ~MyFrame() {}
private:
    wxProcess* recorderProc = nullptr;
    long recorderPid = 0;
    ModernButton* startBtn = nullptr;
    ModernButton* stopBtn = nullptr;
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
