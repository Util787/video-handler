package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20) // equals 20 MB
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

	fmt.Println("\nuploading thumbnail for video", videoID, "by user", userID)

	err = r.ParseMultipartForm(10 << 20) // equals 10 MB
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't parse thumbnail", err)
		return
	}

	// tn = thumbnail
	tnFile, tnFileHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload thumbnail", err)
		return
	}
	defer tnFile.Close()

	mediaType := tnFileHeader.Header.Get("Content-Type")
	// using HasPrefix and HasSuffix instead of mediaType[:6] and mediaType[6:] to avoid panic if len of mediaType value is not long enough.
	if !strings.HasPrefix(mediaType, "image/") || strings.HasSuffix(mediaType, "gif") {
		respondWithError(w, http.StatusBadRequest, "Incorrect media type", err)
		return
	}
	tnFileExtension := filepath.Ext(tnFileHeader.Filename)

	// storing at this path: /assets/<videoID>.<file_extension>
	tnPath := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%s.%s", videoID, tnFileExtension))
	log.Println("tnPath:", tnPath) // to delete

	//deleting old thumbnail before creating a new one so the video has only one thumbnail
	cleanupOldThumbnailIfExists(cfg.assetsRoot, videoID)

	createdFile, err := os.Create(tnPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create thumbnail file", err)
		return
	}
	defer createdFile.Close()

	_, err = io.Copy(createdFile, tnFile)
	if err != nil {
		cleanup(tnPath)
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy thumbnail file", err)
		return
	}

	// tnURL must be this format: http://localhost:<port>/assets/<videoID>.<file_extension>
	tnURL := fmt.Sprintf("http://localhost:%v/assets/%v.%v", cfg.port, videoID, tnFileExtension)
	log.Println("tnURL:", tnURL) // to delete
	video.ThumbnailURL = &tnURL

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", nil)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func cleanup(path string) {
	if err := os.Remove(path); err != nil {
		log.Printf("Failed to cleanup file %s: %v", path, err)
		return
	}
	log.Println("Deleted file:",path)
}

// this func deleting is not depends on file extension
func cleanupOldThumbnailIfExists(assetsRoot string, videoID uuid.UUID) {
	pattern := filepath.Join(assetsRoot, videoID.String()+".*")
	if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
		os.Remove(matches[0])
	}
}
