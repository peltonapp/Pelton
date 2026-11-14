//go:build darwin

#import <Foundation/Foundation.h>
#import <Vision/Vision.h>
#include <stdlib.h>
#include <string.h>

// peltonRecognizeText reads the words in an image with Vision. On success it
// returns a JSON array of {text, x, y, w, h, line} with boxes as fractions of the
// image from its top left; on failure it returns NULL and sets *errOut. The
// caller frees whichever string it gets.
char *peltonRecognizeText(const void *bytes, int length, char **errOut) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:bytes length:(NSUInteger)length];
        VNRecognizeTextRequest *request = [[VNRecognizeTextRequest alloc] init];
        request.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
        request.usesLanguageCorrection = YES;
        if (@available(macOS 13.0, *)) {
            request.automaticallyDetectsLanguage = YES;
        }

        VNImageRequestHandler *handler = [[VNImageRequestHandler alloc] initWithData:data options:@{}];
        NSError *error = nil;
        if (![handler performRequests:@[request] error:&error]) {
            const char *msg = error.localizedDescription.UTF8String;
            *errOut = strdup(msg ? msg : "text recognition failed");
            return NULL;
        }

        NSCharacterSet *space = [NSCharacterSet whitespaceAndNewlineCharacterSet];
        NSMutableArray *words = [NSMutableArray array];
        NSInteger lineIndex = -1;
        for (VNRecognizedTextObservation *line in request.results) {
            VNRecognizedText *best = [[line topCandidates:1] firstObject];
            if (best == nil) {
                continue;
            }
            lineIndex++;
            NSString *text = best.string;
            NSUInteger i = 0, n = text.length;
            // split on whitespace only, so punctuation stays with its word
            // and a copied line reads the way it was printed.
            while (i < n) {
                while (i < n && [space characterIsMember:[text characterAtIndex:i]]) {
                    i++;
                }
                NSUInteger start = i;
                while (i < n && ![space characterIsMember:[text characterAtIndex:i]]) {
                    i++;
                }
                if (i == start) {
                    break;
                }
                NSRange range = NSMakeRange(start, i - start);
                VNRectangleObservation *box = [best boundingBoxForRange:range error:nil];
                CGRect r = box != nil ? box.boundingBox : line.boundingBox;
                [words addObject:@{
                    @"text": [text substringWithRange:range],
                    @"x": @(r.origin.x),
                    @"y": @(1.0 - r.origin.y - r.size.height),
                    @"w": @(r.size.width),
                    @"h": @(r.size.height),
                    @"line": @(lineIndex),
                }];
            }
        }

        NSData *json = [NSJSONSerialization dataWithJSONObject:words options:0 error:&error];
        if (json == nil) {
            *errOut = strdup("text recognition returned no result");
            return NULL;
        }
        char *out = malloc(json.length + 1);
        memcpy(out, json.bytes, json.length);
        out[json.length] = 0;
        return out;
    }
}
