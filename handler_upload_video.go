package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 30<<20) // equals 30 MB
	defer r.Body.Close()

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't find video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User is not the owner of video", nil)
		return
	}

	err = r.ParseMultipartForm(10 << 20) // equals 10 MB
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse video", err)
		return
	}

	fmt.Println("\nuploading videofile for video", videoID, "by user", userID)

	videoFile, videoFileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video", err)
		return
	}
	defer videoFile.Close()

	mediaType := videoFileHeader.Header.Get("Content-Type")
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Incorrect media type", err)
		return
	}

	cleanupOldVideoIfExists(cfg.videosRoot, videoID)

	//path format: /assets/videos/<videoID>.mp4
	videoPath := filepath.Join(cfg.videosRoot, fmt.Sprintf("%s.mp4", videoID))
	log.Println("Old video path:", videoPath) // to delete

	createdFile, err := os.Create(videoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create video file", err)
		return
	}
	defer createdFile.Close()

	_, err = io.Copy(createdFile, videoFile)
	if err != nil {
		cleanup(videoPath)
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy video file", err)
		return
	}

	processedVideoPath, err := processVideoForFastStart(videoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't proccess video file", err)
		return
	}
	log.Println("Processed video path: ", processedVideoPath) // to delete

	// videoURL format: http://localhost:<port>/assets/videos/<videoID>_processed.mp4
	videoURL := fmt.Sprintf("http://localhost:%v/assets/videos/%v_processed.mp4", cfg.port, videoID)
	log.Println("videoURL:", videoURL) // to delete
	video.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", nil)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func cleanupOldVideoIfExists(videosRoot string, videoID uuid.UUID) {
	pattern := filepath.Join(videosRoot, (videoID.String() + "_processed.mp4"))
	if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
		os.Remove(matches[0])
		log.Println("Removed old video: ", matches[0])
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := strings.TrimSuffix(filePath, ".mp4") + "_processed.mp4"

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	// cleanup(filePath) idk how to fix race condition here cuz ffmpeg doesnt let me so I just got rid of it 

	return outputFilePath, nil
}

